package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"tethon/internal/hub"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	addr := flag.String("addr", "127.0.0.1:39180", "listen address")
	hostTimeout := flag.Duration("host-timeout", 30*time.Second, "host online timeout")
	authToken := flag.String("auth-token", os.Getenv("TETHON_HUB_AUTH_TOKEN"), "bearer token required for hub requests")
	authTokens := flag.String("auth-tokens", os.Getenv("TETHON_HUB_AUTH_TOKENS"), "additional bearer tokens accepted for hub requests, separated by comma or newline")
	dbPath := flag.String("db", defaultDBPath(), "sqlite database path")
	flag.Parse()

	hubServer := hub.New(*hostTimeout)
	hubServer.SetAuthTokens(configuredAuthTokens(*authToken, *authTokens)...)
	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		return err
	}
	store, err := hub.OpenDeviceStore(context.Background(), *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	hubServer.SetDeviceStore(store)
	srv := &http.Server{Addr: *addr, Handler: hubServer.Handler()}
	log.Printf("hub serving on http://%s with db %s", *addr, *dbPath)
	return srv.ListenAndServe()
}

func configuredAuthTokens(primary, additional string) []string {
	values := []string{}
	if primary != "" {
		values = append(values, primary)
	}
	if additional != "" {
		values = append(values, additional)
	}
	return hub.ParseAuthTokens(values...)
}

func defaultDBPath() string {
	dir, err := defaultStateDir()
	if err != nil {
		return "./hub.db"
	}
	return filepath.Join(dir, "hub.db")
}

func defaultStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return defaultStateDirFor(runtime.GOOS, home, os.Getenv("XDG_DATA_HOME")), nil
}

func defaultStateDirFor(goos, home, xdgDataHome string) string {
	if goos == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "Tethon", "hub")
	}
	if xdgDataHome != "" {
		return filepath.Join(xdgDataHome, "tethon", "hub")
	}
	return filepath.Join(home, ".local", "share", "tethon", "hub")
}
