// Package codex connects to the native Codex app-server. Codex owns the engine;
// closing a connection does not stop the managed daemon or its threads.
package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const MaxMessageBytes = 8 << 20

type DaemonInfo struct {
	Status           string `json:"status"`
	SocketPath       string `json:"socketPath"`
	CLIVersion       string `json:"cliVersion"`
	AppServerVersion string `json:"appServerVersion"`
}

type Config struct {
	Binary       string
	SocketPath   string
	StartDaemon  bool
	Experimental bool
}

// Message preserves native methods, identifiers, and payloads. An ID on a
// message with a method identifies a server request, not a response to Call.
type Message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// DeliveryError means the request may have reached Codex. Callers must
// reconcile native state before offering a retry of a state-changing request.
type DeliveryError struct{ Err error }

type Client struct {
	conn     *websocket.Conn
	ctx      context.Context
	cancel   context.CancelFunc
	messages chan Message
	done     chan struct{}

	nextID  atomic.Uint64
	mu      sync.Mutex
	pending map[string]chan Message
	err     error
	once    sync.Once
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("Codex (%d): %s", e.Code, e.Message)
}

func (e *DeliveryError) Error() string {
	return "Codex action delivery is uncertain: " + e.Err.Error()
}

func (e *DeliveryError) Unwrap() error {
	return e.Err
}

// SupportedVersion limits the integration to the tested native protocol line.
// A later protocol must be checked before expanding this range.
func SupportedVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 || parts[0] != "0" || parts[1] != "163" {
		return false
	}

	patch, err := strconv.Atoi(parts[2])
	return err == nil && patch >= 0
}

func Discover(ctx context.Context, config Config) (DaemonInfo, error) {
	binary := config.Binary
	if binary == "" {
		binary = "codex"
	}

	readVersion := func() (DaemonInfo, error) {
		var info DaemonInfo
		output, err := exec.CommandContext(ctx, binary, "app-server", "daemon", "version").Output()
		if err != nil {
			return info, fmt.Errorf("inspect managed Codex app-server: %w", err)
		}

		if err := json.Unmarshal(output, &info); err != nil {
			return info, fmt.Errorf("read managed Codex version: %w", err)
		}

		return info, nil
	}

	info, err := readVersion()
	if err != nil {
		return info, err
	}

	if !SupportedVersion(info.CLIVersion) {
		return info, fmt.Errorf("Codex CLI %q is unsupported; this integration requires 0.163.x", info.CLIVersion)
	}

	if info.Status != "running" && config.StartDaemon {
		// Native start is idempotent. Do not restart or stop an existing daemon.
		if err := exec.CommandContext(ctx, binary, "app-server", "daemon", "start").Run(); err != nil {
			return info, fmt.Errorf("start managed Codex app-server: %w", err)
		}

		info, err = readVersion()
		if err != nil {
			return info, err
		}
	}

	if info.Status != "running" {
		return info, errors.New("Codex app-server is offline; run codex app-server daemon start")
	}

	if !SupportedVersion(info.AppServerVersion) {
		return info, fmt.Errorf("Codex app-server %q is unsupported; this integration requires 0.163.x", info.AppServerVersion)
	}

	if config.SocketPath != "" {
		info.SocketPath = config.SocketPath
	}

	if !filepath.IsAbs(info.SocketPath) {
		return info, errors.New("Codex app-server did not provide an absolute local socket path")
	}

	return info, nil
}

func Connect(ctx context.Context, config Config) (*Client, DaemonInfo, error) {
	info, err := Discover(ctx, config)
	if err != nil {
		return nil, info, err
	}

	httpClient := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", info.SocketPath)
		},
	}}

	conn, _, err := websocket.Dial(ctx, "ws://localhost/", &websocket.DialOptions{HTTPClient: httpClient})
	if err != nil {
		return nil, info, fmt.Errorf("connect to local Codex app-server: %w", err)
	}

	client := newClient(conn)
	initialize := map[string]any{
		"clientInfo": map[string]string{"name": "tethon", "title": "Tethon", "version": "0.1.0"},
	}
	if config.Experimental {
		initialize["capabilities"] = map[string]bool{"experimentalApi": true}
	}

	if err := client.Call(ctx, "initialize", initialize, nil); err != nil {
		client.Close()
		return nil, info, err
	}

	if err := client.Notify(ctx, "initialized", struct{}{}); err != nil {
		client.Close()
		return nil, info, err
	}

	return client, info, nil
}

func newClient(conn *websocket.Conn) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	conn.SetReadLimit(MaxMessageBytes)

	c := &Client{
		conn:     conn,
		ctx:      ctx,
		cancel:   cancel,
		messages: make(chan Message, 256),
		done:     make(chan struct{}),
		pending:  make(map[string]chan Message),
	}

	go c.read()

	return c
}

func (c *Client) Messages() <-chan Message { return c.messages }
func (c *Client) Done() <-chan struct{}    { return c.done }

func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.err
}

func (c *Client) Close() { c.fail(errors.New("Codex client disconnected")) }

func (c *Client) fail(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()

		c.cancel()
		_ = c.conn.CloseNow()
		close(c.done)
	})
}

func (c *Client) read() {
	defer close(c.messages)

	for {
		var message Message
		if err := wsjson.Read(c.ctx, c.conn, &message); err != nil {
			c.fail(err)
			return
		}

		if message.Method == "" && len(message.ID) != 0 {
			c.mu.Lock()
			pending := c.pending[string(message.ID)]
			c.mu.Unlock()

			if pending != nil {
				select {
				case pending <- message:
				default:
				}
			}

			continue
		}

		select {
		case c.messages <- message:
		case <-c.done:
			return
		default:
			// Never block RPC replies behind a full notification queue or silently
			// discard native requests. Recovery must reconcile the native thread.
			c.fail(errors.New("Codex event queue is full; reconnect and reconcile native history"))
			return
		}
	}
}

func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	id, _ := json.Marshal(fmt.Sprintf("tethon-%d", c.nextID.Add(1)))
	response := make(chan Message, 1)

	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return err
	}

	c.pending[string(id)] = response
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, string(id))
		c.mu.Unlock()
	}()

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := wsjson.Write(ctx, c.conn, Message{ID: id, Method: method, Params: paramsJSON}); err != nil {
		return &DeliveryError{Err: err}
	}

	select {
	case message := <-response:
		if message.Error != nil {
			return message.Error
		}

		if result != nil {
			return json.Unmarshal(message.Result, result)
		}

		return nil
	case <-ctx.Done():
		return &DeliveryError{Err: ctx.Err()}
	case <-c.done:
		return &DeliveryError{Err: c.Err()}
	}
}

func (c *Client) Notify(ctx context.Context, method string, params any) error {
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}

	return wsjson.Write(ctx, c.conn, Message{Method: method, Params: data})
}

func (c *Client) Respond(ctx context.Context, id json.RawMessage, result any) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}

	return wsjson.Write(ctx, c.conn, Message{ID: id, Result: data})
}
