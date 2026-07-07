//go:build linux

package resource

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"tethon/internal/protocol"
)

type linuxProcessInfo struct {
	pid        int
	ppid       int
	pgid       int
	command    string
	executable string
	rssBytes   int64
	cpuTimeMS  int64
}

func collectSessionProcesses(ctx context.Context, target SessionTarget, roots []PathRoot) ([]protocol.ResourceProcess, []protocol.ResourceListeningPort, []protocol.ResourceWarning) {
	table, warnings := linuxProcessTable(ctx)
	if len(table) == 0 {
		if len(warnings) == 0 {
			warnings = append(warnings, warning("process_scan_empty", "process table scan returned no processes"))
		}
		return nil, nil, warnings
	}
	owned := ownedLinuxPIDs(table, target.Identity.PID, target.Identity.PGID, target.Identity.OwnsProcessGroup)
	processes := make([]protocol.ResourceProcess, 0, len(owned))
	for pid := range owned {
		info := table[pid]
		process := redactCommand(info.command, roots)
		process.PID = info.pid
		process.PPID = info.ppid
		process.PGID = info.pgid
		process.Executable = info.executable
		process.RSSBytes = info.rssBytes
		process.CPUTimeMS = info.cpuTimeMS
		processes = append(processes, process)
	}
	ports, portWarnings := linuxListeningPorts(ctx, owned)
	warnings = append(warnings, portWarnings...)
	return processes, ports, warnings
}

func linuxProcessTable(ctx context.Context) (map[int]linuxProcessInfo, []protocol.ResourceWarning) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, []protocol.ResourceWarning{warning("process_scan_failed", err.Error())}
	}
	table := make(map[int]linuxProcessInfo)
	warnings := []protocol.ResourceWarning{}
	pageSize := int64(os.Getpagesize())
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return table, append(warnings, warning("process_scan_timeout", err.Error()))
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		statRaw, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		stat, err := parseLinuxProcStat(string(statRaw))
		if err != nil {
			continue
		}
		command := linuxCommandLine(entry.Name())
		executable := ""
		if exe, err := os.Readlink(filepath.Join("/proc", entry.Name(), "exe")); err == nil {
			executable = exe
		}
		table[pid] = linuxProcessInfo{
			pid:        stat.PID,
			ppid:       stat.PPID,
			pgid:       stat.PGID,
			command:    command,
			executable: executable,
			rssBytes:   stat.RSSPages * pageSize,
			cpuTimeMS:  linuxCPUTimeMS(stat),
		}
	}
	return table, warnings
}

func linuxCommandLine(pid string) string {
	raw, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
	if err != nil {
		return ""
	}
	value := strings.TrimRight(string(raw), "\x00")
	value = strings.ReplaceAll(value, "\x00", " ")
	return strings.TrimSpace(value)
}

func ownedLinuxPIDs(table map[int]linuxProcessInfo, rootPID, rootPGID int, ownsGroup bool) map[int]struct{} {
	owned := map[int]struct{}{}
	if rootPID > 0 {
		owned[rootPID] = struct{}{}
	}
	if ownsGroup && rootPGID > 0 {
		for pid, info := range table {
			if info.pgid == rootPGID {
				owned[pid] = struct{}{}
			}
		}
	}
	changed := true
	for changed {
		changed = false
		for pid, info := range table {
			if _, ok := owned[pid]; ok {
				continue
			}
			if _, ok := owned[info.ppid]; ok {
				owned[pid] = struct{}{}
				changed = true
			}
		}
	}
	return owned
}

func linuxListeningPorts(ctx context.Context, owned map[int]struct{}) ([]protocol.ResourceListeningPort, []protocol.ResourceWarning) {
	inodeToPID := map[string]int{}
	for pid := range owned {
		if err := ctx.Err(); err != nil {
			return nil, []protocol.ResourceWarning{warning("port_scan_timeout", err.Error())}
		}
		entries, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "fd"))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			link, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "fd", entry.Name()))
			if err != nil {
				continue
			}
			if strings.HasPrefix(link, "socket:[") && strings.HasSuffix(link, "]") {
				inode := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
				inodeToPID[inode] = pid
			}
		}
	}

	ports := []protocol.ResourceListeningPort{}
	warnings := []protocol.ResourceWarning{}
	for _, spec := range []struct {
		path     string
		protocol string
	}{
		{path: "/proc/net/tcp", protocol: "tcp"},
		{path: "/proc/net/tcp6", protocol: "tcp6"},
	} {
		raw, err := os.ReadFile(spec.path)
		if err != nil {
			warnings = append(warnings, warning("port_scan_failed", err.Error()))
			continue
		}
		sockets, err := parseLinuxListenSockets(string(raw), spec.protocol)
		if err != nil {
			warnings = append(warnings, warning("port_scan_failed", err.Error()))
			continue
		}
		for _, socket := range sockets {
			pid, ok := inodeToPID[socket.Inode]
			if !ok {
				continue
			}
			ports = append(ports, protocol.ResourceListeningPort{
				PID:      pid,
				Protocol: socket.Protocol,
				Address:  socket.Address,
				Port:     socket.Port,
			})
		}
	}
	return ports, warnings
}
