package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"tethon/internal/protocol"
	"tethon/internal/server"
	"tethon/internal/sessionlog"
)

const defaultAddr = "127.0.0.1:39180"

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}

	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "session":
		return session(args[1:])
	default:
		return usage()
	}
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", defaultAddr, "listen address")
	dbPath := fs.String("db", "./tethon.db", "sqlite database path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	store, err := sessionlog.Open(context.Background(), *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	srv := &http.Server{Addr: *addr, Handler: server.New(store).Handler()}
	log.Printf("serving on http://%s", *addr)
	return srv.ListenAndServe()
}

func session(args []string) error {
	if len(args) == 0 {
		return usage()
	}

	switch args[0] {
	case "start":
		return sessionStart(args[1:])
	case "stream":
		return sessionStream(args[1:])
	case "input":
		return sessionInput(args[1:])
	case "approve":
		return sessionApproval(args[1:], "approve")
	case "deny":
		return sessionApproval(args[1:], "deny")
	case "reply":
		return sessionApproval(args[1:], "reply")
	default:
		return usage()
	}
}

func sessionStart(args []string) error {
	fs := flag.NewFlagSet("session start", flag.ExitOnError)
	addr := fs.String("addr", defaultAddr, "server address")
	repo := fs.String("repo", "", "repo path")
	cwd := fs.String("cwd", "", "working directory")
	branch := fs.String("branch", "", "branch")
	agent := fs.String("agent", "fake", "agent")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var resp protocol.CreateSessionResponse
	return postJSON("http://"+*addr+"/sessions", protocol.CreateSessionRequest{
		Repo:   *repo,
		CWD:    *cwd,
		Branch: *branch,
		Agent:  *agent,
	}, &resp, os.Stdout)
}

func sessionStream(args []string) error {
	addr, rest, err := stringFlag(args, "addr", defaultAddr)
	if err != nil {
		return err
	}
	from, rest, err := intFlag(rest, "from", 0)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: tethon session stream <id> --from <seq>")
	}

	url := fmt.Sprintf("http://%s/sessions/%s/stream?from=%d", addr, rest[0], from)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		_, _ = io.Copy(os.Stderr, resp.Body)
		return fmt.Errorf("stream failed: %s", resp.Status)
	}
	_, err = io.Copy(os.Stdout, resp.Body)
	return err
}

func sessionInput(args []string) error {
	addr, rest, err := stringFlag(args, "addr", defaultAddr)
	if err != nil {
		return err
	}
	if len(rest) < 2 {
		return fmt.Errorf("usage: tethon session input <id> <text>")
	}
	var resp any
	return postJSON("http://"+addr+"/sessions/"+rest[0]+"/input", protocol.InputRequest{Text: strings.Join(rest[1:], " ")}, &resp, os.Stdout)
}

func sessionApproval(args []string, action string) error {
	addr, rest, err := stringFlag(args, "addr", defaultAddr)
	if err != nil {
		return err
	}
	if len(rest) < 2 {
		return fmt.Errorf("usage: tethon session %s <id> <request_id>", action)
	}
	req := protocol.ResolveApprovalRequest{Action: action}
	if action == "reply" {
		if len(rest) < 3 {
			return fmt.Errorf("usage: tethon session reply <id> <request_id> <text>")
		}
		req.Text = strings.Join(rest[2:], " ")
	}
	var resp any
	return postJSON("http://"+addr+"/sessions/"+rest[0]+"/approvals/"+rest[1], req, &resp, os.Stdout)
}

func postJSON(url string, req any, out any, dst io.Writer) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		_, _ = io.Copy(os.Stderr, resp.Body)
		return fmt.Errorf("request failed: %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return err
	}
	enc := json.NewEncoder(dst)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func stringFlag(args []string, name, defaultValue string) (string, []string, error) {
	value := defaultValue
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--"+name || arg == "-"+name {
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("%s needs a value", arg)
			}
			value = args[i+1]
			i++
			continue
		}
		prefix := "--" + name + "="
		if strings.HasPrefix(arg, prefix) {
			value = strings.TrimPrefix(arg, prefix)
			continue
		}
		rest = append(rest, arg)
	}
	return value, rest, nil
}

func intFlag(args []string, name string, defaultValue int64) (int64, []string, error) {
	rawDefault := strconv.FormatInt(defaultValue, 10)
	raw, rest, err := stringFlag(args, name, rawDefault)
	if err != nil {
		return 0, nil, err
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, nil, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return value, rest, nil
}

func usage() error {
	return fmt.Errorf(`usage:
  tethon serve --addr 127.0.0.1:39180 --db ./tethon.db
  tethon session start --repo <path> --agent fake
  tethon session stream <id> --from 0
  tethon session input <id> <text>
  tethon session approve <id> <request_id>
  tethon session deny <id> <request_id>`)
}
