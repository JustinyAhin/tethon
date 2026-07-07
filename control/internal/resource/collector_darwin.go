//go:build darwin

package resource

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"

	"tethon/internal/protocol"
)

type darwinProcessInfo struct {
	pid       int
	ppid      int
	pgid      int
	rssBytes  int64
	cpuTimeMS int64
	command   string
}

func collectSessionProcesses(ctx context.Context, target SessionTarget, roots []PathRoot) ([]protocol.ResourceProcess, []protocol.ResourceListeningPort, []protocol.ResourceWarning) {
	table, warnings := darwinProcessTable(ctx)
	if len(table) == 0 {
		if len(warnings) == 0 {
			warnings = append(warnings, warning("process_scan_empty", "process table scan returned no processes"))
		}
		return nil, nil, warnings
	}
	owned := ownedDarwinPIDs(table, target.Identity.PID, target.Identity.PGID, target.Identity.OwnsProcessGroup)
	processes := make([]protocol.ResourceProcess, 0, len(owned))
	for pid := range owned {
		info := table[pid]
		process := redactCommand(info.command, roots)
		process.PID = info.pid
		process.PPID = info.ppid
		process.PGID = info.pgid
		process.RSSBytes = info.rssBytes
		process.CPUTimeMS = info.cpuTimeMS
		processes = append(processes, process)
	}
	ports, portWarnings := darwinListeningPorts(ctx, owned)
	warnings = append(warnings, portWarnings...)
	return processes, ports, warnings
}

func darwinProcessTable(ctx context.Context) (map[int]darwinProcessInfo, []protocol.ResourceWarning) {
	cmd := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,pgid=,rss=,time=,command=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, []protocol.ResourceWarning{warning("process_scan_failed", msg)}
	}
	table := map[int]darwinProcessInfo{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		info, ok := parseDarwinProcessLine(line)
		if ok {
			table[info.pid] = info
		}
	}
	return table, nil
}

func parseDarwinProcessLine(line string) (darwinProcessInfo, bool) {
	fields := strings.Fields(line)
	if len(fields) < 6 {
		return darwinProcessInfo{}, false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return darwinProcessInfo{}, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return darwinProcessInfo{}, false
	}
	pgid, err := strconv.Atoi(fields[2])
	if err != nil {
		return darwinProcessInfo{}, false
	}
	rssKB, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return darwinProcessInfo{}, false
	}
	return darwinProcessInfo{
		pid:       pid,
		ppid:      ppid,
		pgid:      pgid,
		rssBytes:  rssKB * 1024,
		cpuTimeMS: parseDarwinCPUTimeMS(fields[4]),
		command:   strings.Join(fields[5:], " "),
	}, true
}

func parseDarwinCPUTimeMS(raw string) int64 {
	daySplit := strings.Split(raw, "-")
	days := int64(0)
	timePart := raw
	if len(daySplit) == 2 {
		if parsed, err := strconv.ParseInt(daySplit[0], 10, 64); err == nil {
			days = parsed
			timePart = daySplit[1]
		}
	}
	parts := strings.Split(timePart, ":")
	if len(parts) != 3 {
		return 0
	}
	hours, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0
	}
	minutes, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0
	}
	seconds, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0
	}
	return (((days*24+hours)*60+minutes)*60 + seconds) * 1000
}

func ownedDarwinPIDs(table map[int]darwinProcessInfo, rootPID, rootPGID int, ownsGroup bool) map[int]struct{} {
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

func darwinListeningPorts(ctx context.Context, owned map[int]struct{}) ([]protocol.ResourceListeningPort, []protocol.ResourceWarning) {
	cmd := exec.CommandContext(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, []protocol.ResourceWarning{warning("port_scan_failed", msg)}
	}
	ports := []protocol.ResourceListeningPort{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		port, ok := parseDarwinLsofLine(line, owned)
		if ok {
			ports = append(ports, port)
		}
	}
	return ports, nil
}

func parseDarwinLsofLine(line string, owned map[int]struct{}) (protocol.ResourceListeningPort, bool) {
	fields := strings.Fields(line)
	if len(fields) < 9 || fields[0] == "COMMAND" {
		return protocol.ResourceListeningPort{}, false
	}
	pid, err := strconv.Atoi(fields[1])
	if err != nil {
		return protocol.ResourceListeningPort{}, false
	}
	if _, ok := owned[pid]; !ok {
		return protocol.ResourceListeningPort{}, false
	}
	name := strings.Join(fields[8:], " ")
	portValue, ok := parseLastColonPort(name)
	if !ok {
		return protocol.ResourceListeningPort{}, false
	}
	return protocol.ResourceListeningPort{PID: pid, Protocol: "tcp", Port: portValue}, true
}

func parseLastColonPort(name string) (int, bool) {
	beforeListen, _, _ := strings.Cut(name, " ")
	index := strings.LastIndex(beforeListen, ":")
	if index < 0 || index == len(beforeListen)-1 {
		return 0, false
	}
	port, err := strconv.Atoi(beforeListen[index+1:])
	if err != nil {
		return 0, false
	}
	return port, true
}
