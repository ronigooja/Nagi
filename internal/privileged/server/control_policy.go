package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ronigooja/Nagi/internal/privileged"
)

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
		return privileged.Response{}, errors.New("invalid reload operation")
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
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
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
	const maxBody = 4 << 20
	// /traffic never finishes. One validated sample per helper call keeps the
	// protocol framed and lets the user CLI retain its existing NDJSON stream.
	var body []byte
	if req.Path == "/traffic" {
		body, err = readTrafficSample(response.Body)
	} else {
		body, err = io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	}
	if err != nil {
		return privileged.Response{}, err
	}
	if len(body) > maxBody {
		return privileged.Response{}, errors.New("controller response too large")
	}
	if len(bytes.TrimSpace(body)) != 0 && !json.Valid(body) {
		return privileged.Response{}, errors.New("invalid controller JSON response")
	}
	return privileged.Response{OK: response.StatusCode >= 200 && response.StatusCode < 300, Status: response.StatusCode, Body: json.RawMessage(body)}, nil
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
		return nil, errors.New("invalid traffic sample")
	}
	return line, nil
}

func validateControl(req privileged.Request) error {
	u, err := url.ParseRequestURI(req.Path)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Fragment != "" || strings.Contains(req.Path, "\\") || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || strings.Contains(strings.ToLower(u.EscapedPath()), "%5c") {
		return errors.New("invalid controller path")
	}
	parts := strings.Split(u.Path, "/")
	for _, part := range parts {
		if part == "." || part == ".." {
			return errors.New("invalid controller path")
		}
	}
	switch req.Method {
	case http.MethodGet:
		if len(req.Body) != 0 {
			return errors.New("GET body is not allowed")
		}
		switch u.Path {
		case "/version", "/configs", "/proxies", "/connections", "/rules", "/providers/rules", "/traffic":
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
		if len(parts) == 4 && parts[1] == "proxies" && parts[3] == "delay" && parts[2] != "" {
			q := u.Query()
			target, targetErr := url.Parse(q.Get("url"))
			timeout, timeoutErr := strconv.Atoi(q.Get("timeout"))
			if len(q) == 2 && targetErr == nil && (target.Scheme == "http" || target.Scheme == "https") && target.Host != "" && target.User == nil && timeoutErr == nil && timeout >= 1 && timeout <= 30000 {
				return nil
			}
		}
	case http.MethodPost:
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
	case http.MethodPut:
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
	return fmt.Errorf("controller operation %s %s is not allowed", req.Method, req.Path)
}

func strictJSON(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}

func validateConfigPatch(data []byte) error {
	var fields map[string]json.RawMessage
	if err := strictJSON(data, &fields); err != nil || len(fields) == 0 {
		return errors.New("invalid configuration patch")
	}
	for key, value := range fields {
		switch key {
		case "mode":
			var mode string
			if strictJSON(value, &mode) != nil || mode != "rule" && mode != "global" && mode != "direct" {
				return errors.New("invalid mode")
			}
		case "allow-lan":
			var enabled bool
			if strictJSON(value, &enabled) != nil {
				return errors.New("invalid allow-lan")
			}
		case "bind-address":
			var address string
			if strictJSON(value, &address) != nil || address != "*" && net.ParseIP(address) == nil {
				return errors.New("invalid bind-address")
			}
		case "tun":
			var tun struct {
				Enable              *bool `json:"enable"`
				AutoRoute           *bool `json:"auto-route"`
				AutoDetectInterface *bool `json:"auto-detect-interface"`
			}
			if strictJSON(value, &tun) != nil || tun.Enable == nil {
				return errors.New("invalid tun patch")
			}
		default:
			return errors.New("configuration patch field is not allowed")
		}
	}
	return nil
}
