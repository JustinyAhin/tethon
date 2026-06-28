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
	dbPath := flag.String("db", "./hostd.db", "sqlite database path")
	hostID := flag.String("host-id", sanitizeHostID(hostname), "host id")
	name := flag.String("name", hostname, "host display name")
	allowAnthropicAPIKey := flag.Bool("allow-anthropic-api-key", false, "allow claude-cli sessions while ANTHROPIC_API_KEY is set")
	worktreeRoot := flag.String("worktree-root", "", "directory for per-session worktrees (default: <db dir>/worktrees)")
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
	worktrees := worktree.NewManager(wtRoot)
	logger := slog.Default()

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
		}))
	}

	hostURL := "http://" + *addr
	registerReq := protocol.RegisterHostRequest{
		HostID:    *hostID,
		Name:      *name,
		URL:       hostURL,
		Agents:    agentRegistry.Names(),
		RepoRoots: registry.Paths(),
		Repos:     repoInfos(registry),
	}
	if err := registerHost(*hubURL, registerReq); err != nil {
		log.Printf("initial host registration failed: %v", err)
	}
	go heartbeatLoop(*hubURL, registerReq)

	srv := &http.Server{Addr: *addr, Handler: server.NewWithConfig(store, server.Config{
		Agents:    agentRegistry,
		Repos:     registry,
		Worktrees: worktrees,
		RepoRoots: registry.Paths(),
		Logger:    logger,
	}).Handler()}
	log.Printf("hostd %s serving on %s and registering with %s", *hostID, hostURL, *hubURL)
	return srv.ListenAndServe()
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

func heartbeatLoop(hubURL string, req protocol.RegisterHostRequest) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if err := heartbeat(hubURL, req.HostID); err != nil {
			if registerErr := registerHost(hubURL, req); registerErr != nil {
				log.Printf("heartbeat failed: %v; register failed: %v", err, registerErr)
			}
		}
	}
}

func registerHost(hubURL string, req protocol.RegisterHostRequest) error {
	return postJSON(strings.TrimRight(hubURL, "/")+"/hosts/register", req)
}

func heartbeat(hubURL, hostID string) error {
	return postJSON(strings.TrimRight(hubURL, "/")+"/hosts/"+hostID+"/heartbeat", map[string]string{})
}

func postJSON(target string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	resp, err := http.Post(target, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned %s", target, resp.Status)
	}
	return nil
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
