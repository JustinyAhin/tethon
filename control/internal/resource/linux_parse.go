package resource

import (
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
)

const linuxClockTicksPerSecond = 100

type linuxProcStat struct {
	PID      int
	PPID     int
	PGID     int
	UTime    int64
	STime    int64
	RSSPages int64
}

type linuxListenSocket struct {
	Protocol string
	Address  string
	Port     int
	Inode    string
}

func parseLinuxProcStat(raw string) (linuxProcStat, error) {
	open := strings.Index(raw, "(")
	close := strings.LastIndex(raw, ")")
	if open < 0 || close < open {
		return linuxProcStat{}, fmt.Errorf("invalid proc stat")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(raw[:open]))
	if err != nil {
		return linuxProcStat{}, err
	}
	fields := strings.Fields(strings.TrimSpace(raw[close+1:]))
	if len(fields) < 22 {
		return linuxProcStat{}, fmt.Errorf("short proc stat")
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return linuxProcStat{}, err
	}
	pgid, err := strconv.Atoi(fields[2])
	if err != nil {
		return linuxProcStat{}, err
	}
	utime, err := strconv.ParseInt(fields[11], 10, 64)
	if err != nil {
		return linuxProcStat{}, err
	}
	stime, err := strconv.ParseInt(fields[12], 10, 64)
	if err != nil {
		return linuxProcStat{}, err
	}
	rssPages, err := strconv.ParseInt(fields[21], 10, 64)
	if err != nil {
		return linuxProcStat{}, err
	}
	return linuxProcStat{PID: pid, PPID: ppid, PGID: pgid, UTime: utime, STime: stime, RSSPages: rssPages}, nil
}

func parseLinuxListenSockets(raw, protocol string) ([]linuxListenSocket, error) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) <= 1 {
		return nil, nil
	}
	sockets := []linuxListenSocket{}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		if fields[3] != "0A" {
			continue
		}
		address, port, err := parseLinuxAddress(fields[1], protocol)
		if err != nil {
			return nil, err
		}
		sockets = append(sockets, linuxListenSocket{
			Protocol: protocol,
			Address:  address,
			Port:     port,
			Inode:    fields[9],
		})
	}
	return sockets, nil
}

func parseLinuxAddress(raw, protocol string) (string, int, error) {
	hostHex, portHex, ok := strings.Cut(raw, ":")
	if !ok {
		return "", 0, fmt.Errorf("invalid local address")
	}
	port64, err := strconv.ParseInt(portHex, 16, 32)
	if err != nil {
		return "", 0, err
	}
	switch protocol {
	case "tcp":
		if len(hostHex) != 8 {
			return "", 0, fmt.Errorf("invalid ipv4 address")
		}
		value, err := strconv.ParseUint(hostHex, 16, 32)
		if err != nil {
			return "", 0, err
		}
		ip := net.IPv4(byte(value), byte(value>>8), byte(value>>16), byte(value>>24))
		return ip.String(), int(port64), nil
	case "tcp6":
		bytes, err := hex.DecodeString(hostHex)
		if err != nil {
			return "", 0, err
		}
		if len(bytes) != net.IPv6len {
			return "", 0, fmt.Errorf("invalid ipv6 address")
		}
		for i := 0; i < len(bytes); i += 4 {
			bytes[i], bytes[i+3] = bytes[i+3], bytes[i]
			bytes[i+1], bytes[i+2] = bytes[i+2], bytes[i+1]
		}
		return net.IP(bytes).String(), int(port64), nil
	default:
		return "", 0, fmt.Errorf("unsupported protocol")
	}
}

func linuxCPUTimeMS(stat linuxProcStat) int64 {
	return (stat.UTime + stat.STime) * 1000 / linuxClockTicksPerSecond
}
