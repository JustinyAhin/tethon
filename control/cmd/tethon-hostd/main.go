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
	"sync"
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
	if len(os.Args) > 1 && os.Args[1] == "claude-hook" {
		return agent.RunClaudeHook(os.Stdin, os.Stdout, os.Getenv)
	}

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
	logger.Info("hostd_state_paths", "host_id", *hostID, "db", *dbPath, "worktree_root", wtRoot, "cache_root", cRoot)

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
	status := newHostStatus(hostStatusConfig{
		hostID:        *hostID,
		advertisedURL: hostURL,
		hubURL:        *hubURL,
		repos:         registerReq.Repos,
	})
	hostServer := server.NewWithConfig(store, server.Config{
		Agents:    agentRegistry,
		Repos:     registry,
		Worktrees: worktrees,
		RepoRoots: registry.Paths(),
		Logger:    logger,
	})
	if err := hostServer.Recover(ctx); err != nil {
		logger.Error("session_recovery_failed", "error", err.Error())
	}

	go runHubConnectionLoop(ctx, hub, registerReq, hubConnectionConfig{
		hubURL: *hubURL,
		logger: logger,
		status: status,
	})
	go runHubReverseLoop(ctx, hub, registerReq.HostID, hostServer.Handler(), hubReverseConfig{
		hubURL: *hubURL,
		logger: logger,
		status: status,
	})

	srv := &http.Server{Addr: *addr, Handler: hostDiagnosticHandler(hostServer.Handler(), status)}
	logger.Info("hostd_serving", "host_id", *hostID, "advertised_url", hostURL, "hub_url", *hubURL)
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

type hostStatusConfig struct {
	hostID        string
	advertisedURL string
	hubURL        string
	repos         []protocol.RepoInfo
	now           func() time.Time
}

type hostStatus struct {
	mu            sync.RWMutex
	hostID        string
	advertisedURL string
	hubURL        string
	repos         []protocol.RepoInfo
	startedAt     time.Time
	now           func() time.Time

	registered         bool
	retrying           bool
	lastHubSuccess     time.Time
	lastHubFailure     *hubFailure
	nextRetryDelay     time.Duration
	reverseConnected   bool
	lastReverseFailure *hubFailure
}

type hubFailure struct {
	at    time.Time
	error string
}

type hostStatusResponse struct {
	Status             string                `json:"status"`
	HostID             string                `json:"host_id"`
	AdvertisedURL      string                `json:"advertised_url"`
	HubURL             string                `json:"hub_url"`
	RepoCount          int                   `json:"repo_count"`
	Repos              []hostStatusRepo      `json:"repos"`
	UptimeSeconds      int64                 `json:"uptime_seconds"`
	Registered         bool                  `json:"registered"`
	Retrying           bool                  `json:"retrying"`
	LastHubSuccess     string                `json:"last_hub_success"`
	LastHubFailure     *hostStatusHubFailure `json:"last_hub_failure"`
	NextRetryDelay     string                `json:"next_retry_delay"`
	ReverseConnected   bool                  `json:"reverse_connected"`
	LastReverseFailure *hostStatusHubFailure `json:"last_reverse_failure"`
}

type hostStatusRepo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type hostStatusHubFailure struct {
	At    string `json:"at"`
	Error string `json:"error"`
}

func newHostStatus(cfg hostStatusConfig) *hostStatus {
	now := cfg.now
	if now == nil {
		now = time.Now
	}
	repos := make([]protocol.RepoInfo, len(cfg.repos))
	copy(repos, cfg.repos)
	return &hostStatus{
		hostID:        cfg.hostID,
		advertisedURL: cfg.advertisedURL,
		hubURL:        cfg.hubURL,
		repos:         repos,
		startedAt:     now().UTC(),
		now:           now,
	}
}

func (s *hostStatus) recordHubSuccess() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registered = true
	s.retrying = false
	s.lastHubSuccess = s.now().UTC()
	s.nextRetryDelay = 0
}

func (s *hostStatus) recordHubFailure(err error, nextRetryDelay time.Duration, registered bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registered = registered
	s.retrying = true
	s.lastHubFailure = &hubFailure{at: s.now().UTC(), error: err.Error()}
	s.nextRetryDelay = nextRetryDelay
}

func (s *hostStatus) recordHubForgotHost() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registered = false
	s.retrying = true
	s.nextRetryDelay = 0
}

func (s *hostStatus) recordReverseConnected() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reverseConnected = true
}

func (s *hostStatus) recordReverseFailure(err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reverseConnected = false
	s.lastReverseFailure = &hubFailure{at: s.now().UTC(), error: err.Error()}
}

func (s *hostStatus) snapshot() hostStatusResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := s.now().UTC()
	repos := make([]hostStatusRepo, 0, len(s.repos))
	for _, repo := range s.repos {
		repos = append(repos, hostStatusRepo{ID: repo.ID, Name: repo.Name})
	}
	resp := hostStatusResponse{
		Status:           "ok",
		HostID:           s.hostID,
		AdvertisedURL:    s.advertisedURL,
		HubURL:           s.hubURL,
		RepoCount:        len(s.repos),
		Repos:            repos,
		UptimeSeconds:    int64(now.Sub(s.startedAt).Seconds()),
		Registered:       s.registered,
		Retrying:         s.retrying,
		NextRetryDelay:   durationString(s.nextRetryDelay),
		ReverseConnected: s.reverseConnected,
	}
	if !s.lastHubSuccess.IsZero() {
		resp.LastHubSuccess = s.lastHubSuccess.Format(time.RFC3339)
	}
	if s.lastHubFailure != nil {
		resp.LastHubFailure = &hostStatusHubFailure{
			At:    s.lastHubFailure.at.Format(time.RFC3339),
			Error: s.lastHubFailure.error,
		}
	}
	if s.lastReverseFailure != nil {
		resp.LastReverseFailure = &hostStatusHubFailure{
			At:    s.lastReverseFailure.at.Format(time.RFC3339),
			Error: s.lastReverseFailure.error,
		}
	}
	return resp
}

func durationString(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.String()
}

func hostDiagnosticHandler(next http.Handler, status *hostStatus) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz" && r.Method == http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case r.URL.Path == "/status" && r.Method == http.MethodGet:
			writeJSON(w, http.StatusOK, status.snapshot())
		case r.URL.Path == "/healthz" || r.URL.Path == "/status":
			writeJSON(w, http.StatusMethodNotAllowed, protocol.ErrorResponse{Error: "method not allowed"})
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
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
	hubURL            string
	logger            *slog.Logger
	status            *hostStatus
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
	if c.logger == nil {
		c.logger = slog.Default()
	}
	return c
}

func runHubConnectionLoop(ctx context.Context, hub hostHub, req protocol.RegisterHostRequest, cfg hubConnectionConfig) {
	cfg = cfg.withDefaults()
	if cfg.status == nil {
		cfg.status = newHostStatus(hostStatusConfig{
			hostID:        req.HostID,
			advertisedURL: req.URL,
			hubURL:        cfg.hubURL,
			repos:         req.Repos,
		})
	}
	registered := false
	registrationFailures := 0
	heartbeatFailures := 0
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
			cfg.status.recordHubSuccess()
			if !registered {
				cfg.logger.Info(
					"host_registered",
					append(hubLogAttrs(cfg, req),
						"failed_attempts", registrationFailures,
						"next_heartbeat_delay", cfg.heartbeatInterval.String(),
					)...,
				)
				registrationFailures = 0
			} else if heartbeatFailures > 0 {
				cfg.logger.Info(
					"hub_heartbeat_recovered",
					append(hubLogAttrs(cfg, req),
						"failed_attempts", heartbeatFailures,
						"next_heartbeat_delay", cfg.heartbeatInterval.String(),
					)...,
				)
				heartbeatFailures = 0
			}
			registered = true
			retryDelay = cfg.minBackoff
			nextDelay = cfg.heartbeatInterval
			continue
		}

		if registered && isHTTPStatus(err, http.StatusNotFound) {
			cfg.logger.Warn("hub_forgot_host", hubLogAttrs(cfg, req)...)
			cfg.status.recordHubForgotHost()
			registered = false
			heartbeatFailures = 0
			retryDelay = cfg.minBackoff
			nextDelay = 0
			continue
		}

		delay := retryDelay + cfg.jitter(retryDelay)
		if registered {
			heartbeatFailures++
			cfg.status.recordHubFailure(err, delay, true)
			cfg.logger.Warn(
				"hub_heartbeat_failed",
				append(hubLogAttrs(cfg, req),
					"error", err.Error(),
					"failed_attempts", heartbeatFailures,
					"next_retry_delay", delay.String(),
				)...,
			)
		} else {
			registrationFailures++
			cfg.status.recordHubFailure(err, delay, false)
			cfg.logger.Warn(
				"host_registration_failed",
				append(hubLogAttrs(cfg, req),
					"error", err.Error(),
					"failed_attempts", registrationFailures,
					"next_retry_delay", delay.String(),
				)...,
			)
		}
		nextDelay = delay
		retryDelay = nextBackoff(retryDelay, cfg.maxBackoff)
	}
}

func hubLogAttrs(cfg hubConnectionConfig, req protocol.RegisterHostRequest) []any {
	return []any{
		"hub_url", cfg.hubURL,
		"host_id", req.HostID,
		"advertised_url", req.URL,
		"repo_count", len(req.Repos),
		"agent_count", len(req.Agents),
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
