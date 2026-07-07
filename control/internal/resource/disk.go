package resource

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"

	"tethon/internal/protocol"
)

func collectWorktreeDiskUsage(ctx context.Context, path string) (*protocol.ResourceDiskUsage, *protocol.ResourceWarning) {
	if err := ctx.Err(); err != nil {
		w := warning("disk_usage_timeout", err.Error())
		return nil, &w
	}
	cmd := exec.CommandContext(ctx, "du", "-sk", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		w := warning("disk_usage_failed", msg)
		return nil, &w
	}
	fields := strings.Fields(stdout.String())
	if len(fields) == 0 {
		w := warning("disk_usage_failed", "du returned no output")
		return nil, &w
	}
	kbytes, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		w := warning("disk_usage_failed", "du returned invalid size")
		return nil, &w
	}
	return &protocol.ResourceDiskUsage{Path: path, Bytes: kbytes * 1024}, nil
}
