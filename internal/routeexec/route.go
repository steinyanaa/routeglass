package routeexec

import (
	"bufio"
	"context"
	"errors"
	"net/netip"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Hop struct {
	Number    int     `json:"hop"`
	Address   string  `json:"address,omitempty"`
	LatencyMS float64 `json:"latency_ms,omitempty"`
}

var hopLine = regexp.MustCompile(`^\s*(\d+)\s+(?:\S+\s+)?(\d+\.\d+\.\d+\.\d+|[0-9a-fA-F:]+|\*)\s+.*?([0-9.]+)\s*ms`)

func Run(ctx context.Context, target string) ([]Hop, string, error) {
	a, e := netip.ParseAddr(target)
	if e != nil {
		return nil, "", errors.New("target must be an IP address")
	}
	target = a.Unmap().String()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	type routeAttempt struct {
		name  string
		label string
		args  []string
	}
	var attempts []routeAttempt
	if runtime.GOOS == "windows" {
		attempts = []routeAttempt{{name: "tracert", label: "icmp", args: []string{"-d", "-h", "20", "-w", "1000", target}}}
	} else {
		attempts = []routeAttempt{
			{name: "traceroute", label: "icmp", args: []string{"-I", "-n", "-m", "20", "-w", "1", "-q", "1", target}},
			{name: "traceroute", label: "udp", args: []string{"-n", "-m", "20", "-w", "1", "-q", "1", target}},
			{name: "traceroute", label: "tcp/443", args: []string{"-T", "-p", "443", "-n", "-m", "20", "-w", "1", "-q", "1", target}},
		}
	}
	var lastErr error
	for _, attempt := range attempts {
		cmd := exec.CommandContext(ctx, attempt.name, attempt.args...)
		out, runErr := cmd.Output()
		if len(out) > 256*1024 {
			out = out[:256*1024]
		}
		hops := parseHops(out)
		if len(hops) > 0 || runErr == nil {
			return hops, attempt.label, nil
		}
		lastErr = runErr
		if ctx.Err() != nil {
			break
		}
	}
	return nil, "unavailable", lastErr
}

func parseHops(out []byte) []Hop {
	hops := []Hop{}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		m := hopLine.FindStringSubmatch(sc.Text())
		if len(m) == 4 {
			n, _ := strconv.Atoi(m[1])
			ms, _ := strconv.ParseFloat(m[3], 64)
			addr := m[2]
			if addr == "*" {
				addr = ""
			}
			hops = append(hops, Hop{n, addr, ms})
		}
	}
	return hops
}
