package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	"tethon/internal/protocol"
	"tethon/internal/reverse"
)

var errReverseUnavailable = errors.New("host reverse connection is unavailable")

type reverseConnection struct {
	server *Server
	hostID string
	conn   *websocket.Conn

	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[string]*reversePending

	done     chan struct{}
	doneOnce sync.Once
}

type reversePending struct {
	conn      *reverseConnection
	requestID string
	start     chan reverse.Frame
	body      chan []byte
	done      chan reverse.Frame

	closeOnce sync.Once
}

func newReverseConnection(server *Server, hostID string, conn *websocket.Conn) *reverseConnection {
	return &reverseConnection{
		server:  server,
		hostID:  hostID,
		conn:    conn,
		pending: make(map[string]*reversePending),
		done:    make(chan struct{}),
	}
}

func (c *reverseConnection) run(ctx context.Context) error {
	defer c.close(websocket.StatusNormalClosure, "reverse connection closed")
	for {
		var frame reverse.Frame
		if err := wsjson.Read(ctx, c.conn, &frame); err != nil {
			c.failAll(err)
			return err
		}
		c.dispatch(frame)
	}
}

func (c *reverseConnection) dispatch(frame reverse.Frame) {
	c.pendingMu.Lock()
	pending := c.pending[frame.RequestID]
	c.pendingMu.Unlock()
	if pending == nil {
		return
	}
	switch frame.Type {
	case reverse.FrameResponseStart:
		pending.start <- frame
	case reverse.FrameResponseBody:
		pending.body <- frame.Body
	case reverse.FrameResponseEnd, reverse.FrameResponseError:
		pending.done <- frame
	}
}

func (c *reverseConnection) startRequest(ctx context.Context, method, path, query string, body []byte) (*reversePending, error) {
	if c.closed() {
		return nil, errReverseUnavailable
	}
	requestID := uuid.NewString()
	pending := &reversePending{
		conn:      c,
		requestID: requestID,
		start:     make(chan reverse.Frame, 1),
		body:      make(chan []byte, 16),
		done:      make(chan reverse.Frame, 1),
	}
	c.pendingMu.Lock()
	c.pending[requestID] = pending
	c.pendingMu.Unlock()
	if err := c.writeFrame(ctx, reverse.Frame{
		Type:      reverse.FrameRequest,
		RequestID: requestID,
		Method:    method,
		Path:      path,
		Query:     query,
		Body:      body,
	}); err != nil {
		pending.close(false)
		return nil, err
	}
	return pending, nil
}

func (c *reverseConnection) writeFrame(ctx context.Context, frame reverse.Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return wsjson.Write(ctx, c.conn, frame)
}

func (c *reverseConnection) close(status websocket.StatusCode, reason string) {
	c.doneOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close(status, reason)
		c.failAll(errReverseUnavailable)
	})
}

func (c *reverseConnection) closed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *reverseConnection) failAll(err error) {
	c.pendingMu.Lock()
	pending := make([]*reversePending, 0, len(c.pending))
	for _, p := range c.pending {
		pending = append(pending, p)
	}
	c.pending = make(map[string]*reversePending)
	c.pendingMu.Unlock()

	frame := reverse.Frame{Type: reverse.FrameResponseError, Error: err.Error()}
	for _, p := range pending {
		p.done <- frame
	}
}

func (p *reversePending) close(sendCancel bool) {
	p.closeOnce.Do(func() {
		p.conn.pendingMu.Lock()
		delete(p.conn.pending, p.requestID)
		p.conn.pendingMu.Unlock()
		if sendCancel {
			_ = p.conn.writeFrame(context.Background(), reverse.Frame{Type: reverse.FrameRequestCancel, RequestID: p.requestID})
		}
	})
}

func (p *reversePending) waitStart(ctx context.Context) (reverse.Frame, error) {
	select {
	case start := <-p.start:
		return start, nil
	case done := <-p.done:
		return reverse.Frame{}, responseFrameError(done)
	case <-ctx.Done():
		p.close(true)
		return reverse.Frame{}, ctx.Err()
	}
}

func responseFrameError(frame reverse.Frame) error {
	if frame.Error != "" {
		return errors.New(frame.Error)
	}
	return errReverseUnavailable
}

func (s *Server) setReverseConnection(hostID string, conn *reverseConnection) {
	s.mu.Lock()
	old := s.reverse[hostID]
	s.reverse[hostID] = conn
	if record, ok := s.hosts[hostID]; ok {
		record.LastSeen = s.now().UTC()
		s.hosts[hostID] = record
	}
	s.mu.Unlock()
	if old != nil {
		old.close(websocket.StatusNormalClosure, "replaced by new reverse connection")
	}
}

func (s *Server) removeReverseConnection(hostID string, conn *reverseConnection) {
	s.mu.Lock()
	if s.reverse[hostID] == conn {
		delete(s.reverse, hostID)
	}
	s.mu.Unlock()
}

func (s *Server) reverseConnection(hostID string) (*reverseConnection, bool) {
	s.mu.RLock()
	conn := s.reverse[hostID]
	s.mu.RUnlock()
	if conn == nil || conn.closed() {
		return nil, false
	}
	return conn, true
}

func (s *Server) reverseRequestRaw(ctx context.Context, hostID, method, path, query string, body []byte) (int, []byte, error) {
	conn, ok := s.reverseConnection(hostID)
	if !ok {
		return 0, nil, errReverseUnavailable
	}
	pending, err := conn.startRequest(ctx, method, path, query, body)
	if err != nil {
		return 0, nil, err
	}
	defer pending.close(false)

	start, err := pending.waitStart(ctx)
	if err != nil {
		return 0, nil, err
	}

	var out bytes.Buffer
	for {
		select {
		case chunk := <-pending.body:
			_, _ = out.Write(chunk)
		case done := <-pending.done:
			drainBody(&out, pending)
			if done.Type == reverse.FrameResponseError {
				if done.Status > 0 {
					return done.Status, errorBody(done.Error), nil
				}
				return 0, nil, responseFrameError(done)
			}
			return start.Status, out.Bytes(), nil
		case <-ctx.Done():
			pending.close(true)
			return 0, nil, ctx.Err()
		}
	}
}

func drainBody(out *bytes.Buffer, pending *reversePending) {
	for {
		select {
		case chunk := <-pending.body:
			_, _ = out.Write(chunk)
		default:
			return
		}
	}
}

func (s *Server) reverseStream(ctx context.Context, hostID, path, query string) (*reversePending, reverse.Frame, error) {
	conn, ok := s.reverseConnection(hostID)
	if !ok {
		return nil, reverse.Frame{}, errReverseUnavailable
	}
	pending, err := conn.startRequest(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return nil, reverse.Frame{}, err
	}
	start, err := pending.waitStart(ctx)
	if err != nil {
		pending.close(false)
		return nil, reverse.Frame{}, err
	}
	return pending, start, nil
}

func writeReverseStream(ctx context.Context, writer io.Writer, flusher http.Flusher, pending *reversePending) {
	sendCancel := true
	defer func() {
		pending.close(sendCancel)
	}()
	for {
		select {
		case chunk := <-pending.body:
			if _, err := writer.Write(chunk); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		case <-pending.done:
			sendCancel = false
			drainStreamBody(writer, flusher, pending)
			return
		case <-ctx.Done():
			return
		}
	}
}

func drainStreamBody(writer io.Writer, flusher http.Flusher, pending *reversePending) {
	for {
		select {
		case chunk := <-pending.body:
			_, _ = writer.Write(chunk)
			if flusher != nil {
				flusher.Flush()
			}
		default:
			return
		}
	}
}

func errorBody(message string) []byte {
	if message == "" {
		message = http.StatusText(http.StatusInternalServerError)
	}
	body, err := json.Marshal(protocol.ErrorResponse{Error: message})
	if err != nil {
		return []byte(fmt.Sprintf(`{"error":%q}`, message))
	}
	return body
}
