package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"tethon/internal/codex"
	"tethon/internal/protocol"
)

func sessionAttach(args []string) error {
	hubURL, authToken, rest, err := hubFlags(args)
	if err != nil {
		return err
	}

	if len(rest) != 1 {
		return fmt.Errorf("usage: tethon session attach <host_id/session_id> (run on that host)")
	}

	var snapshot protocol.CodexSessionResponse
	endpoint := hubEndpoint(hubURL, "/sessions/"+rest[0]+"/native")
	if err := getJSON(endpoint, authToken, &snapshot, io.Discard); err != nil {
		return err
	}

	if _, err := uuid.Parse(snapshot.State.ThreadID); err != nil {
		return fmt.Errorf("the session has no supported native Codex thread")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	info, err := codex.Discover(ctx, codex.Config{})
	if err != nil {
		return err
	}

	localSocket, err := filepath.EvalSymlinks(info.SocketPath)
	if err != nil {
		return fmt.Errorf("the local managed Codex socket is unavailable")
	}

	sessionSocket, err := filepath.EvalSymlinks(snapshot.State.SocketPath)
	if err != nil || localSocket != sessionSocket {
		return fmt.Errorf("run this command on the selected host to attach its native Codex terminal")
	}

	command := exec.Command("codex", "resume", "--remote", "unix://"+localSocket, snapshot.State.ThreadID)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	return command.Run()
}
