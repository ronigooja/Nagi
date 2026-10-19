package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/privileged"
	"github.com/ronigooja/Nagi/internal/rules"

	"github.com/ronigooja/Nagi/internal/i18n"
)

// snapshotControl is narrower than the controller authorization allowlist.
// Measurement GETs can update automatic group selection and DNS queries change
// resolver state; they retain the mutation lock. New endpoints default to serial.
func snapshotControl(req privileged.Request) bool {
	if req.Method != http.MethodGet || validateControl(req) != nil {
		return false
	}
	u, _ := url.ParseRequestURI(req.Path)
	switch u.Path {
	case "/version", "/configs", "/proxies", "/providers/proxies", "/connections", "/rules", "/providers/rules", "/traffic", "/request-rewrite/capabilities", "/request-rewrite/status":
		return true
	}
	parts := strings.Split(u.Path, "/")
	return u.RawQuery == "" && (len(parts) == 3 && parts[1] == "proxies" && parts[2] != "" || len(parts) == 4 && parts[1] == "providers" && parts[2] == "proxies" && parts[3] != "")
}

// controlRequest accepts only operations needed by the CLI. The root
// controller socket is never returned to the client.
func controlRequest(ctx context.Context, socket string, req privileged.Request) (privileged.Response, error) {
	if err := validateControl(req); err != nil {
		return privileged.Response{}, err
	}
	return sendControl(ctx, socket, req)
}

func controlRequestReload(ctx context.Context, socket string, req privileged.Request) (privileged.Response, error) {
	if req.Method != http.MethodPut || req.Path != "/configs?force=true" {
		return privileged.Response{}, i18n.Errorf("system.privileged.invalid.reload.operation", "invalid reload operation")
	}
	return sendControl(ctx, socket, req)
}

func sendControl(ctx context.Context, socket string, req privileged.Request) (privileged.Response, error) {
	if err := VerifyControllerSocket(socket); err != nil {
		return privileged.Response{}, err
	}
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	timeout := 10 * time.Second
	if strings.Contains(req.Path, "/delay?") || strings.Contains(req.Path, "/healthcheck?") {
		// The validated mihomo measurement may consume its full 30 seconds.
		timeout = 31 * time.Second
	}
	client := &http.Client{Transport: transport, Timeout: timeout}
	request, err := http.NewRequestWithContext(ctx, req.Method, "http://nagi.invalid"+req.Path, bytes.NewReader(req.Body))
	if err != nil {
		return privileged.Response{}, err
	}
	if len(req.Body) != 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return privileged.Response{}, err
	}
	defer response.Body.Close()
	// /traffic never finishes. One validated sample per helper call keeps the
	// protocol framed and lets the user CLI retain its existing NDJSON stream.
	var body []byte
	if req.Path == "/traffic" {
		body, err = readTrafficSample(response.Body)
	} else {
		body, err = io.ReadAll(io.LimitReader(response.Body, control.MaxResponseBytes+1))
	}
	if err != nil {
		return privileged.Response{}, err
	}
	if len(body) > control.MaxResponseBytes {
		return privileged.Response{}, control.ErrResponseTooLarge
	}
	if u, parseErr := url.ParseRequestURI(req.Path); parseErr == nil && isRewritePath(u.Path) && response.StatusCode == http.StatusNotFound {
		return privileged.Response{Status: http.StatusNotFound, Body: json.RawMessage(`{"error":{"code":"rewrite_capability_missing","message":"Install the matching mihomo engine with request rewrite support"}}`)}, nil
	}
	if len(bytes.TrimSpace(body)) != 0 && !json.Valid(body) {
		return privileged.Response{}, i18n.Errorf("system.privileged.invalid.controller.json.response", "invalid controller JSON response")
	}
	return filterRewriteResponse(req, privileged.Response{OK: response.StatusCode >= 200 && response.StatusCode < 300, Status: response.StatusCode, Body: json.RawMessage(body)})
}

func readTrafficSample(r io.Reader) ([]byte, error) {
	line := make([]byte, 0, 256)
	var one [1]byte
	for len(line) <= 4096 {
		n, err := r.Read(one[:])
		if n == 1 {
			if one[0] == '\n' {
				break
			}
			line = append(line, one[0])
		}
		if err != nil {
			return nil, err
		}
	}
	if len(line) > 4096 || !json.Valid(line) {
		return nil, i18n.Errorf("system.privileged.invalid.traffic.sample", "invalid traffic sample")
	}
	return line, nil
}

func validateControl(req privileged.Request) error {
	u, err := url.ParseRequestURI(req.Path)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Fragment != "" || strings.Contains(req.Path, "\\") || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || strings.Contains(strings.ToLower(u.EscapedPath()), "%5c") {
		return i18n.Errorf("system.privileged.invalid.controller.path", "invalid controller path")
	}
	if isRewritePath(u.Path) {
		return validateRewriteControl(req, u)
	}
	parts := strings.Split(u.Path, "/")
	for _, part := range parts {
		if part == "." || part == ".." {
			return i18n.Errorf("system.privileged.invalid.controller.path", "invalid controller path")
		}
	}
	switch req.Method {
	case http.MethodGet:
		if len(req.Body) != 0 {
			return i18n.Errorf("system.privileged.get.body.is.not.allowed", "GET body is not allowed")
		}
		switch u.Path {
		case "/version", "/configs", "/proxies", "/providers/proxies", "/connections", "/rules", "/providers/rules", "/traffic":
			if u.RawQuery == "" {
				return nil
			}
		case "/dns/query":
			q := u.Query()
			if len(q) == 2 && q.Get("name") != "" && (q.Get("type") == "A" || q.Get("type") == "AAAA") {
				return nil
			}
		}
		if len(parts) == 3 && parts[1] == "proxies" && parts[2] != "" && u.RawQuery == "" {
			return nil
		}
		if len(parts) == 4 && parts[1] == "providers" && parts[2] == "proxies" && parts[3] != "" && u.RawQuery == "" {
			return nil
		}
		if len(parts) == 5 && parts[1] == "providers" && parts[2] == "proxies" && parts[3] != "" && parts[4] == "healthcheck" && u.RawQuery == "" {
			return nil
		}
		if (len(parts) == 4 && (parts[1] == "proxies" || parts[1] == "group") && parts[3] == "delay" && parts[2] != "") || (len(parts) == 6 && parts[1] == "providers" && parts[2] == "proxies" && parts[3] != "" && parts[4] != "" && parts[5] == "healthcheck") {
			q := u.Query()
			target, targetErr := url.Parse(q.Get("url"))
			timeout, timeoutErr := strconv.Atoi(q.Get("timeout"))
			_, hasExpected := q["expected"]
			knownFields := len(q) == 2 || len(q) == 3 && hasExpected
			// Mihomo parses the expected-status ranges. The helper only bounds
			// this optional value and rejects additional or repeated fields.
			if knownFields && len(q["url"]) == 1 && len(q["timeout"]) == 1 && len(q["expected"]) <= 1 && len(q.Get("expected")) <= 1024 && targetErr == nil && (target.Scheme == "http" || target.Scheme == "https") && target.Host != "" && target.User == nil && timeoutErr == nil && timeout >= 1 && timeout <= 30000 {
				return nil
			}
		}
	case http.MethodPost:
		if u.Path == "/rules/test" && u.RawQuery == "" && len(req.Body) <= 4096 {
			var body rules.TestRequest
			if strictJSON(req.Body, &body) == nil && body.Validate() == nil {
				return nil
			}
		}
		if u.Path == "/cache/dns/flush" && u.RawQuery == "" && len(req.Body) == 0 {
			return nil
		}
	case http.MethodDelete:
		if len(req.Body) != 0 || u.RawQuery != "" {
			break
		}
		if u.Path == "/connections" || len(parts) == 3 && parts[1] == "connections" && parts[2] != "" {
			return nil
		}
		if len(parts) == 3 && parts[1] == "proxies" && parts[2] != "" {
			return nil
		}
	case http.MethodPut:
		if len(parts) == 4 && parts[1] == "providers" && parts[2] == "proxies" && parts[3] != "" && u.RawQuery == "" && len(req.Body) == 0 {
			return nil
		}
		if len(parts) == 3 && parts[1] == "proxies" && parts[2] != "" && u.RawQuery == "" {
			var body struct {
				Name string `json:"name"`
			}
			if strictJSON(req.Body, &body) == nil && body.Name != "" {
				return nil
			}
		}
	case http.MethodPatch:
		if u.Path == "/configs" && u.RawQuery == "" {
			return validateConfigPatch(req.Body)
		}
	}
	return i18n.Errorf("system.privileged.controller.operation.is.not.allowed", "controller operation %s %s is not allowed", req.Method, req.Path)
}

func strictJSON(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return i18n.Errorf("system.privileged.multiple.json.values", "multiple JSON values")
	}
	return nil
}

func validateConfigPatch(data []byte) error {
	var fields map[string]json.RawMessage
	if err := strictJSON(data, &fields); err != nil || len(fields) == 0 {
		return i18n.Errorf("system.privileged.invalid.configuration.patch", "invalid configuration patch")
	}
	for key, value := range fields {
		switch key {
		case "mode":
			var mode string
			if strictJSON(value, &mode) != nil || mode != "rule" && mode != "global" && mode != "direct" {
				return i18n.Errorf("system.privileged.invalid.mode", "invalid mode")
			}
		case "allow-lan":
			var enabled bool
			if strictJSON(value, &enabled) != nil {
				return i18n.Errorf("system.privileged.invalid.allow.lan", "invalid allow-lan")
			}
		case "bind-address":
			var address string
			if strictJSON(value, &address) != nil || address != "*" && net.ParseIP(address) == nil {
				return i18n.Errorf("system.privileged.invalid.bind.address", "invalid bind-address")
			}
		case "tun":
			var tun struct {
				Enable              *bool     `json:"enable"`
				AutoRoute           *bool     `json:"auto-route"`
				AutoDetectInterface *bool     `json:"auto-detect-interface"`
				StrictRoute         *bool     `json:"strict-route"`
				DNSHijack           *[]string `json:"dns-hijack"`
			}
			if strictJSON(value, &tun) != nil || tun.Enable == nil {
				return i18n.Errorf("system.privileged.invalid.tun.patch", "invalid tun patch")
			}
			if tun.DNSHijack != nil {
				if len(*tun.DNSHijack) > 32 {
					return i18n.Errorf("system.privileged.too.many.dns.interception.entries", "too many DNS interception entries")
				}
				for _, endpoint := range *tun.DNSHijack {
					endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "udp://"), "tcp://")
					if endpoint == "any:53" {
						continue
					}
					addr, err := netip.ParseAddrPort(endpoint)
					if err != nil || addr.Port() != 53 {
						return i18n.Errorf("system.privileged.dns.interception.must.use.literal.ip.addresses.on.port.53", "DNS interception must use literal IP addresses on port 53")
					}
				}
			}
		default:
			return i18n.Errorf("system.privileged.configuration.patch.field.is.not.allowed", "configuration patch field is not allowed")
		}
	}
	return nil
}
