package command

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type dnsPacketSample struct {
	Status            string            `json:"status"`
	Domain            string            `json:"domain,omitempty"`
	CaptureInterfaces []string          `json:"capture_interfaces"`
	MatchedInterfaces []string          `json:"matched_interfaces"`
	ResolverRoutes    map[string]string `json:"resolver_routes"`
	QueryAttempted    bool              `json:"query_attempted"`
	Issues            []string          `json:"issues"`
}

type dnsPacketRunner interface {
	Interfaces() ([]string, error)
	CaptureAndResolve(context.Context, []string, string) (map[string]string, bool, error)
	RouteTo(context.Context, string) (string, error)
}

func packetSample(ctx context.Context, observation systemDNSObservation, runner dnsPacketRunner) dnsPacketSample {
	result := dnsPacketSample{Status: "unavailable", CaptureInterfaces: []string{}, MatchedInterfaces: []string{}, ResolverRoutes: map[string]string{}, Issues: []string{}}
	interfaces, err := runner.Interfaces()
	if err != nil {
		result.Issues = append(result.Issues, err.Error())
		return result
	}
	if len(interfaces) == 0 {
		result.Issues = append(result.Issues, "no physical capture interface found")
		return result
	}
	result.CaptureInterfaces = interfaces
	for _, resolver := range observation.Resolvers {
		ip := net.ParseIP(resolver)
		if ip == nil {
			continue
		}
		if route, err := runner.RouteTo(ctx, resolver); err == nil && route != "" {
			result.ResolverRoutes[resolver] = route
		} else {
			result.Issues = append(result.Issues, "route to a configured resolver unavailable")
		}
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		result.Issues = append(result.Issues, "cannot create unique query name")
		return result
	}
	result.Domain = "nagi-" + hex.EncodeToString(nonce[:]) + ".example.com"
	captured, attempted, err := runner.CaptureAndResolve(ctx, interfaces, result.Domain)
	result.QueryAttempted = attempted
	if err != nil {
		result.Issues = append(result.Issues, err.Error())
		return result
	}
	if !attempted {
		result.Issues = append(result.Issues, "system resolver query did not start")
		return result
	}
	for _, iface := range interfaces {
		if tcpdumpQuerySeen(captured[iface], result.Domain) {
			result.MatchedInterfaces = append(result.MatchedInterfaces, iface)
		}
	}
	if len(result.MatchedInterfaces) > 0 {
		result.Status = "plaintext_seen"
	} else {
		result.Status = "no_plaintext_seen"
	}
	return result
}

func tcpdumpQuerySeen(output, domain string) bool {
	target := strings.ToLower(domain) + "."
	for _, line := range strings.Split(output, "\n") {
		line = strings.ToLower(line)
		if strings.Contains(line, target) && strings.Contains(line, "? ") {
			return true
		}
	}
	return false
}

type realDNSPacketRunner struct{}

func (realDNSPacketRunner) Interfaces() ([]string, error) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return nil, errors.New("packet sampling supports Linux and macOS")
	}
	if os.Geteuid() != 0 {
		return nil, errors.New("packet sampling requires root privileges")
	}
	if _, err := exec.LookPath("tcpdump"); err != nil {
		return nil, errors.New("tcpdump is required for packet sampling")
	}
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("getent"); err != nil {
			return nil, errors.New("getent is required for the system resolver query")
		}
	} else {
		if _, err := exec.LookPath("dscacheutil"); err != nil {
			return nil, errors.New("dscacheutil is required for the system resolver query")
		}
	}
	all, err := net.Interfaces()
	if err != nil {
		return nil, errors.New("network interfaces unavailable")
	}
	result := []string{}
	for _, iface := range all {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || strings.HasPrefix(iface.Name, "tun") || strings.HasPrefix(iface.Name, "utun") {
			continue
		}
		physical := false
		if runtime.GOOS == "linux" {
			_, err := os.Stat(filepath.Join("/sys/class/net", iface.Name, "device"))
			physical = err == nil
		} else {
			physical = strings.HasPrefix(iface.Name, "en") && len(iface.HardwareAddr) > 0
		}
		if physical {
			result = append(result, iface.Name)
		}
	}
	if len(result) > 8 {
		return nil, errors.New("more than eight physical interfaces; packet sample is unavailable")
	}
	return result, nil
}

func (realDNSPacketRunner) RouteTo(ctx context.Context, ip string) (string, error) {
	addr := net.ParseIP(ip)
	if addr == nil {
		return "", errors.New("invalid resolver address")
	}
	var data string
	var ok bool
	if runtime.GOOS == "linux" {
		data, ok = dnsReadCommand(ctx, "ip", "route", "get", ip)
		if ok {
			fields := strings.Fields(data)
			for i := 0; i+1 < len(fields); i++ {
				if fields[i] == "dev" {
					return fields[i+1], nil
				}
			}
		}
	} else {
		family := "-inet"
		if addr.To4() == nil {
			family = "-inet6"
		}
		data, ok = dnsReadCommand(ctx, "route", "-n", "get", family, ip)
		if ok {
			return macRouteInterface(data), nil
		}
	}
	return "", errors.New("resolver route unavailable")
}

type boundedDNSBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedDNSBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		b.overflow = true
		return 0, errors.New("capture output limit reached")
	}
	return b.Buffer.Write(p)
}

type dnsCaptureProcess struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	output *boundedDNSBuffer
}

func (realDNSPacketRunner) CaptureAndResolve(ctx context.Context, interfaces []string, domain string) (map[string]string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	captures := []dnsCaptureProcess{}
	defer func() {
		for _, capture := range captures {
			capture.cancel()
			_ = capture.cmd.Wait()
		}
	}()
	for _, iface := range interfaces {
		captureCtx, stop := context.WithCancel(ctx)
		cmd := exec.CommandContext(captureCtx, "tcpdump", "-n", "-l", "-s", "512", "-i", iface, "udp port 53 or tcp port 53")
		output := &boundedDNSBuffer{limit: 1 << 20}
		cmd.Stdout = output
		stderr, err := cmd.StderrPipe()
		if err != nil {
			stop()
			return nil, false, errors.New("tcpdump pipe unavailable")
		}
		ready := make(chan bool, 1)
		go func() {
			scanner := bufio.NewScanner(stderr)
			signaled := false
			for scanner.Scan() {
				if !signaled && strings.Contains(scanner.Text(), "listening on") {
					ready <- true
					signaled = true
				}
			}
			if !signaled {
				ready <- false
			}
		}()
		if err := cmd.Start(); err != nil {
			stop()
			return nil, false, errors.New("tcpdump could not start")
		}
		captures = append(captures, dnsCaptureProcess{cmd: cmd, cancel: stop, output: output})
		select {
		case ok := <-ready:
			if !ok {
				return nil, false, errors.New("tcpdump did not become ready")
			}
		case <-time.After(2 * time.Second):
			return nil, false, errors.New("tcpdump readiness timed out")
		case <-ctx.Done():
			return nil, false, errors.New("packet sample timed out")
		}
	}
	queryCtx, queryCancel := context.WithTimeout(ctx, 4*time.Second)
	defer queryCancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "linux" {
		cmd = exec.CommandContext(queryCtx, "getent", "ahosts", domain)
	} else {
		cmd = exec.CommandContext(queryCtx, "dscacheutil", "-q", "host", "-a", "name", domain)
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, false, errors.New("system resolver query could not start")
	}
	_ = cmd.Wait() // NXDOMAIN is expected; the query attempt is the evidence.
	if queryCtx.Err() != nil {
		return nil, true, errors.New("system resolver query timed out")
	}
	select {
	case <-time.After(400 * time.Millisecond):
	case <-ctx.Done():
		return nil, true, errors.New("packet sample timed out")
	}
	for _, capture := range captures {
		capture.cancel()
	}
	result := map[string]string{}
	for _, capture := range captures {
		_ = capture.cmd.Wait()
		if capture.output.overflow {
			return nil, true, errors.New("capture output limit reached")
		}
		result[capture.cmd.Args[6]] = capture.output.String()
	}
	captures = nil
	return result, true, nil
}
