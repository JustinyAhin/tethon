package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"tethon/internal/agent"
	"tethon/internal/protocol"
	"tethon/internal/repo"
	"tethon/internal/server"
	"tethon/internal/sessionlog"
	"tethon/internal/worktree"
)

const (
	defaultHubRequestTimeout = 5 * time.Second
	hubHeartbeatInterval     = 10 * time.Second
	hubRetryMinBackoff       = time.Second
	hubRetryMaxBackoff       = 30 * time.Second
)

type repoRootsFlag []string

func (f *repoRootsFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *repoRootsFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	addr := flag.String("addr", "127.0.0.1:39181", "listen address")
	hubURL := flag.String("hub-url", "http://127.0.0.1:39180", "hub URL")
	hubAuthToken := flag.String("hub-auth-token", os.Getenv("TETHON_HUB_AUTH_TOKEN"), "bearer token for hub requests")
	hubRequestTimeout := flag.Duration("hub-request-timeout", defaultHubRequestTimeout, "timeout for host-to-hub register and heartbeat requests")
	dbPath := flag.String("db", defaultDBPath(), "sqlite database path")
	hostID := flag.String("host-id", sanitizeHostID(hostname), "host id")
	name := flag.String("name", hostname, "host display name")
	allowAnthropicAPIKey := flag.Bool("allow-anthropic-api-key", false, "allow claude-cli sessions while ANTHROPIC_API_KEY is set")
	worktreeRoot := flag.String("worktree-root", "", "directory for per-session worktrees (default: <db dir>/worktrees)")
	cacheRoot := flag.String("cache-root", "", "shared build/dependency cache root for agents (default: <db dir>/cache)")
	var repoRoots repoRootsFlag
	flag.Var(&repoRoots, "repo-root", "repo root to advertise; repeatable")
	flag.Parse()
	if len(repoRoots) == 0 {
		repoRoots = append(repoRoots, cwd)
	}
	ctx := context.Background()
	repos, err := repo.Discover(ctx, repoRoots)
	if err != nil {
		return err
	}
	registry, err := repo.NewRegistry(repos...)
	if err != nil {
		return err
	}
	wtRoot := *worktreeRoot
	if wtRoot == "" {
		wtRoot = filepath.Join(filepath.Dir(*dbPath), "worktrees")
	}
	if wtRoot, err = filepath.Abs(wtRoot); err != nil {
		return err
	}
	if err := os.MkdirAll(wtRoot, 0o755); err != nil {
		return err
	}
	worktrees := worktree.NewManager(wtRoot)

	cRoot := *cacheRoot
	if cRoot == "" {
		cRoot = filepath.Join(filepath.Dir(*dbPath), "cache")
	}
	if cRoot, err = filepath.Abs(cRoot); err != nil {
		return err
	}
	if err := os.MkdirAll(cRoot, 0o755); err != nil {
		return err
	}
	logger := slog.Default()

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		return err
	}
	store, err := sessionlog.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	agentRegistry := agent.NewRegistry(agent.NewFake())
	if _, err := exec.LookPath("claude"); err == nil {
		agentRegistry = agent.NewRegistry(agent.NewFake(), agent.NewClaude(agent.ClaudeConfig{
			AllowAnthropicAPIKey: *allowAnthropicAPIKey,
			Logger:               logger,
			ExtraEnv:             cacheEnv(cRoot),
		}))
	}
	log.Printf("hostd %s using db %s, worktree root %s, and shared cache root %s", *hostID, *dbPath, wtRoot, cRoot)

	hostURL := "http://" + *addr
	registerReq := protocol.RegisterHostRequest{
		HostID:    *hostID,
		Name:      *name,
		URL:       hostURL,
		Agents:    agentRegistry.Names(),
		RepoRoots: registry.Paths(),
		Repos:     repoInfos(registry),
	}
	hub := newHubClient(*hubURL, *hubAuthToken, &http.Client{Timeout: *hubRequestTimeout})
	go runHubConnectionLoop(ctx, hub, registerReq, hubConnectionConfig{})

	hostServer := server.NewWithConfig(store, server.Config{
		Agents:    agentRegistry,
		Repos:     registry,
		Worktrees: worktrees,
		RepoRoots: registry.Paths(),
		Logger:    logger,
	})
	if err := hostServer.Recover(ctx); err != nil {
		log.Printf("session recovery failed: %v", err)
	}

	srv := &http.Server{Addr: *addr, Handler: hostServer.Handler()}
	log.Printf("hostd %s serving on %s and registering with %s", *hostID, hostURL, *hubURL)
	return srv.ListenAndServe()
}

// defaultDBPath returns the default sqlite path inside the per-user app-data
// directory so hostd state does not depend on the process working directory.
// It falls back to ./hostd.db if the app-data dir cannot be resolved.
func defaultDBPath() string {
	dir, err := defaultStateDir()
	if err != nil {
		return "./hostd.db"
	}
	return filepath.Join(dir, "hostd.db")
}

// defaultStateDir resolves the per-user app-data directory for hostd:
// macOS ~/Library/Application Support/Tethon/hostd, Linux
// $XDG_DATA_HOME/tethon/hostd (or ~/.local/share/tethon/hostd).
func defaultStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return defaultStateDirFor(runtime.GOOS, home, os.Getenv("XDG_DATA_HOME")), nil
}

func defaultStateDirFor(goos, home, xdgDataHome string) string {
	if goos == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "Tethon", "hostd")
	}
	if xdgDataHome != "" {
		return filepath.Join(xdgDataHome, "tethon", "hostd")
	}
	return filepath.Join(home, ".local", "share", "tethon", "hostd")
}

// cacheEnv builds the shared build/dependency cache environment for agent
// processes. Each var points at a subdir of root so worktrees reuse one cache
// instead of duplicating it. All are content-addressed or shared stores that
// are safe across worktrees; vars are harmless to repos that don't use them.
func cacheEnv(root string) []string {
	return []string{
		"GOCACHE=" + filepath.Join(root, "go-build"),
		"GOMODCACHE=" + filepath.Join(root, "go-mod"),
		"npm_config_cache=" + filepath.Join(root, "npm"),
		"PIP_CACHE_DIR=" + filepath.Join(root, "pip"),
		"CARGO_HOME=" + filepath.Join(root, "cargo"),
	}
}

func repoInfos(registry *repo.Registry) []protocol.RepoInfo {
	repos := registry.List()
	infos := make([]protocol.RepoInfo, 0, len(repos))
	for _, r := range repos {
		infos = append(infos, protocol.RepoInfo{
			ID:            r.ID,
			Name:          r.Name,
			Path:          r.Path,
			DefaultBranch: r.DefaultBranch,
		})
	}
	return infos
}

type hostHub interface {
	registerHost(context.Context, protocol.RegisterHostRequest) error
	heartbeat(context.Context, string) error
}

type hubConnectionConfig struct {
	heartbeatInterval time.Duration
	minBackoff        time.Duration
	maxBackoff        time.Duration
	jitter            func(time.Duration) time.Duration
	logf              func(string, ...any)
}

func (c hubConnectionConfig) withDefaults() hubConnectionConfig {
	if c.heartbeatInterval <= 0 {
		c.heartbeatInterval = hubHeartbeatInterval
	}
	if c.minBackoff <= 0 {
		c.minBackoff = hubRetryMinBackoff
	}
	if c.maxBackoff <= 0 {
		c.maxBackoff = hubRetryMaxBackoff
	}
	if c.maxBackoff < c.minBackoff {
		c.maxBackoff = c.minBackoff
	}
	if c.jitter == nil {
		c.jitter = defaultBackoffJitter
	}
	if c.logf == nil {
		c.logf = log.Printf
	}
	return c
}

func runHubConnectionLoop(ctx context.Context, hub hostHub, req protocol.RegisterHostRequest, cfg hubConnectionConfig) {
	cfg = cfg.withDefaults()
	registered := false
	retryDelay := cfg.minBackoff
	nextDelay := time.Duration(0)

	for {
		if nextDelay > 0 {
			if !sleepContext(ctx, nextDelay) {
				return
			}
		}

		var err error
		if registered {
			err = hub.heartbeat(ctx, req.HostID)
		} else {
			err = hub.registerHost(ctx, req)
		}

		if err == nil {
			if !registered {
				cfg.logf("host registered with hub")
			}
			registered = true
			retryDelay = cfg.minBackoff
			nextDelay = cfg.heartbeatInterval
			continue
		}

		if registered && isHTTPStatus(err, http.StatusNotFound) {
			cfg.logf("hub forgot host %s; re-registering", req.HostID)
			registered = false
			retryDelay = cfg.minBackoff
			nextDelay = 0
			continue
		}

		delay := retryDelay + cfg.jitter(retryDelay)
		if registered {
			cfg.logf("hub heartbeat failed: %v; retrying in %s", err, delay)
		} else {
			cfg.logf("host registration failed: %v; retrying in %s", err, delay)
		}
		nextDelay = delay
		retryDelay = nextBackoff(retryDelay, cfg.maxBackoff)
	}
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextBackoff(current, max time.Duration) time.Duration {
	if current <= 0 {
		current = hubRetryMinBackoff
	}
	next := current * 2
	if next < current || next > max {
		return max
	}
	return next
}

func defaultBackoffJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	max := base / 5
	if max <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(max) + 1))
}

type hubClient struct {
	baseURL   string
	authToken string
	client    *http.Client
}

func newHubClient(baseURL, authToken string, client *http.Client) *hubClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &hubClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		authToken: authToken,
		client:    client,
	}
}

func (c *hubClient) registerHost(ctx context.Context, req protocol.RegisterHostRequest) error {
	return c.postJSON(ctx, c.baseURL+"/hosts/register", req)
}

func (c *hubClient) heartbeat(ctx context.Context, hostID string) error {
	return c.postJSON(ctx, c.baseURL+"/hosts/"+hostID+"/heartbeat", map[string]string{})
}

func (c *hubClient) postJSON(ctx context.Context, target string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	setBearerAuth(req, c.authToken)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return &httpStatusError{target: target, statusCode: resp.StatusCode, status: resp.Status}
	}
	return nil
}

type httpStatusError struct {
	target     string
	statusCode int
	status     string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("%s returned %s", e.target, e.status)
}

func isHTTPStatus(err error, code int) bool {
	var statusErr *httpStatusError
	return errors.As(err, &statusErr) && statusErr.statusCode == code
}

func postJSON(target string, value any, authToken string) error {
	return newHubClient("", authToken, http.DefaultClient).postJSON(context.Background(), target, value)
}

func setBearerAuth(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func sanitizeHostID(value string) string {
	value = strings.ToLower(value)
	value = regexp.MustCompile(`[^a-z0-9_-]+`).ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "host"
	}
	return value
}
