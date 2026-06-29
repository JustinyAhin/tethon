package main

import (
	"bytes"
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
)

const defaultHubURL = "http://127.0.0.1:39180"

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
	case "hosts":
		return hosts(args[1:])
	case "session":
		return session(args[1:])
	default:
		return usage()
	}
}

func hosts(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "list":
		return hostsList(args[1:])
	default:
		return usage()
	}
}

func hostsList(args []string) error {
	hubURL, authToken, rest, err := hubFlags(args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("usage: tethon hosts list")
	}
	var resp any
	return getJSON(hubEndpoint(hubURL, "/hosts"), authToken, &resp, os.Stdout)
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
	case "cancel":
		return sessionCancel(args[1:])
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
	hubURL := fs.String("hub-url", defaultHubURL, "hub URL")
	authToken := fs.String("auth-token", os.Getenv("TETHON_HUB_AUTH_TOKEN"), "bearer token for hub requests")
	hostID := fs.String("host", "", "host id")
	repo := fs.String("repo", "", "repo path")
	cwd := fs.String("cwd", "", "working directory")
	branch := fs.String("branch", "", "branch")
	agent := fs.String("agent", "fake", "agent")
	prompt := fs.String("prompt", "", "initial prompt")
	permissionMode := fs.String("permission-mode", "", "Claude permission mode")
	if err := fs.Parse(args); err != nil {
		return err
	}
	initialPrompt := *prompt
	if initialPrompt == "" && fs.NArg() > 0 {
		initialPrompt = strings.Join(fs.Args(), " ")
	}

	var resp protocol.CreateSessionResponse
	return postJSON(hubEndpoint(*hubURL, "/sessions"), *authToken, protocol.CreateSessionRequest{
		HostID:         *hostID,
		Repo:           *repo,
		CWD:            *cwd,
		Branch:         *branch,
		Agent:          *agent,
		Prompt:         initialPrompt,
		PermissionMode: *permissionMode,
	}, &resp, os.Stdout)
}

func sessionStream(args []string) error {
	hubURL, authToken, rest, err := hubFlags(args)
	if err != nil {
		return err
	}
	from, rest, err := intFlag(rest, "from", 0)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: tethon session stream <host_id/session_id> --from <seq>")
	}

	url := fmt.Sprintf("%s?from=%d", hubEndpoint(hubURL, "/sessions/"+rest[0]+"/stream"), from)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	setBearerAuth(req, authToken)
	resp, err := http.DefaultClient.Do(req)
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
	hubURL, authToken, rest, err := hubFlags(args)
	if err != nil {
		return err
	}
	if len(rest) < 2 {
		return fmt.Errorf("usage: tethon session input <host_id/session_id> <text>")
	}
	var resp any
	return postJSON(hubEndpoint(hubURL, "/sessions/"+rest[0]+"/input"), authToken, protocol.InputRequest{Text: strings.Join(rest[1:], " ")}, &resp, os.Stdout)
}

func sessionCancel(args []string) error {
	hubURL, authToken, rest, err := hubFlags(args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: tethon session cancel <host_id/session_id>")
	}
	var resp any
	return postJSON(hubEndpoint(hubURL, "/sessions/"+rest[0]+"/cancel"), authToken, map[string]string{}, &resp, os.Stdout)
}

func sessionApproval(args []string, action string) error {
	hubURL, authToken, rest, err := hubFlags(args)
	if err != nil {
		return err
	}
	if len(rest) < 2 {
		return fmt.Errorf("usage: tethon session %s <host_id/session_id> <request_id>", action)
	}
	req := protocol.ResolveApprovalRequest{Action: action}
	if action == "reply" {
		if len(rest) < 3 {
			return fmt.Errorf("usage: tethon session reply <host_id/session_id> <request_id> <text>")
		}
		req.Text = strings.Join(rest[2:], " ")
	}
	var resp any
	return postJSON(hubEndpoint(hubURL, "/sessions/"+rest[0]+"/approvals/"+rest[1]), authToken, req, &resp, os.Stdout)
}

func getJSON(url string, authToken string, out any, dst io.Writer) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	setBearerAuth(req, authToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		_, _ = io.Copy(os.Stderr, resp.Body)
		return fmt.Errorf("request failed: %s", resp.Status)
	}
	return decodeAndPrint(resp.Body, out, dst)
}

func postJSON(url string, authToken string, value any, out any, dst io.Writer) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
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
		_, _ = io.Copy(os.Stderr, resp.Body)
		return fmt.Errorf("request failed: %s", resp.Status)
	}
	return decodeAndPrint(resp.Body, out, dst)
}

func hubFlags(args []string) (hubURL string, authToken string, rest []string, err error) {
	hubURL, rest, err = stringFlag(args, "hub-url", defaultHubURL)
	if err != nil {
		return "", "", nil, err
	}
	authToken, rest, err = stringFlag(rest, "auth-token", os.Getenv("TETHON_HUB_AUTH_TOKEN"))
	if err != nil {
		return "", "", nil, err
	}
	return hubURL, authToken, rest, nil
}

func hubEndpoint(hubURL, path string) string {
	return strings.TrimRight(hubURL, "/") + path
}

func setBearerAuth(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func decodeAndPrint(src io.Reader, out any, dst io.Writer) error {
	if err := json.NewDecoder(src).Decode(out); err != nil {
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
  tethon hosts list [--hub-url <url>] [--auth-token <token>]
  tethon session start --host <host_id> --repo <path> --agent fake [--prompt <text>] [--hub-url <url>] [--auth-token <token>]
  tethon session stream <host_id/session_id> --from 0 [--hub-url <url>] [--auth-token <token>]
  tethon session input <host_id/session_id> <text> [--hub-url <url>] [--auth-token <token>]
  tethon session cancel <host_id/session_id> [--hub-url <url>] [--auth-token <token>]
  tethon session approve <host_id/session_id> <request_id> [--hub-url <url>] [--auth-token <token>]
  tethon session deny <host_id/session_id> <request_id> [--hub-url <url>] [--auth-token <token>]

  --hub-url defaults to ` + defaultHubURL)
}
