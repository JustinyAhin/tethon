package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
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
	if err := registerHost(*hubURL, registerReq, *hubAuthToken); err != nil {
		log.Printf("initial host registration failed: %v", err)
	}
	go heartbeatLoop(*hubURL, registerReq, *hubAuthToken)

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

// defaultStateDir resolves the per-user app-data directory for tethon:
// macOS ~/Library/Application Support/tethon, Linux $XDG_DATA_HOME/tethon
// (or ~/.local/share/tethon).
func defaultStateDir() (string, error) {
	if runtime.GOOS == "darwin" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "tethon"), nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "tethon"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "tethon"), nil
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

func heartbeatLoop(hubURL string, req protocol.RegisterHostRequest, authToken string) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if err := heartbeat(hubURL, req.HostID, authToken); err != nil {
			if registerErr := registerHost(hubURL, req, authToken); registerErr != nil {
				log.Printf("heartbeat failed: %v; register failed: %v", err, registerErr)
			}
		}
	}
}

func registerHost(hubURL string, req protocol.RegisterHostRequest, authToken string) error {
	return postJSON(strings.TrimRight(hubURL, "/")+"/hosts/register", req, authToken)
}

func heartbeat(hubURL, hostID, authToken string) error {
	return postJSON(strings.TrimRight(hubURL, "/")+"/hosts/"+hostID+"/heartbeat", map[string]string{}, authToken)
}

func postJSON(target string, value any, authToken string) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	setBearerAuth(req, authToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned %s", target, resp.Status)
	}
	return nil
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
