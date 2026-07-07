//go:build !darwin && !linux

package resource

import (
	"context"

	"tethon/internal/protocol"
)

func collectSessionProcesses(_ context.Context, _ SessionTarget, _ []PathRoot) ([]protocol.ResourceProcess, []protocol.ResourceListeningPort, []protocol.ResourceWarning) {
	return nil, nil, []protocol.ResourceWarning{
		warning("unsupported_platform", "resource process collection is only implemented on macOS and Linux"),
	}
}
