package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ronigooja/Nagi/internal/capture"
	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/privileged"
	"github.com/ronigooja/Nagi/internal/requestrewrite"
	"golang.org/x/net/publicsuffix"
	"path/filepath"
	"sort"
)

const rewriteControlLimit = 256 << 10

var errRewritePolicy = errors.New("request rewrite controller operation rejected")

type rewritePrepare = control.RewritePrepare

// The control contract contains only flat scalar fields and a string array.
// Decode each field once, with exact spelling, before typed decoding. This also
// rejects nested objects, case aliases, null scalars and duplicate JSON keys.
func rewriteObject(data []byte, keys ...string) (map[string]json.RawMessage, error) {
	if len(data) > rewriteControlLimit {
		return nil, errRewritePolicy
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, errRewritePolicy
	}
	fields := map[string]json.RawMessage{}
	allowed := map[string]bool{}
	for _, key := range keys {
		allowed[key] = true
	}
	for dec.More() {
		tok, err = dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok || !allowed[key] || fields[key] != nil {
			return nil, errRewritePolicy
		}
		var raw json.RawMessage
		if dec.Decode(&raw) != nil {
			return nil, errRewritePolicy
		}

		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && key != "previous_generation" {
			return nil, errRewritePolicy
		}
		fields[key] = raw
	}
	if tok, err = dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, errRewritePolicy
	}
	if _, err = dec.Token(); err != io.EOF || len(fields) != len(keys) {
		return nil, errRewritePolicy
	}
	return fields, nil
}
func rewriteHex(s string, n int) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(s) == n && hex.EncodeToString(b) == s
}
func rewriteSecret(s string) bool {
	b, e := base64.RawURLEncoding.DecodeString(s)
	return e == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == s
}
func rewriteCertificateValid(s string) bool {
	if len(s) > 8192 {
		return false
	}
	der, e := base64.StdEncoding.DecodeString(s)
	if e != nil || base64.StdEncoding.EncodeToString(der) != s {
		return false
	}
	c, e := x509.ParseCertificate(der)
	if e != nil {
		return false
	}
	pub, ok := c.PublicKey.(*ecdsa.PublicKey)
	server, client := false, false
	for _, eku := range c.ExtKeyUsage {
		server = server || eku == x509.ExtKeyUsageServerAuth
		client = client || eku == x509.ExtKeyUsageClientAuth
	}
	now := time.Now()
	return ok && pub.Curve == elliptic.P256() && server && client && c.NotAfter.Sub(c.NotBefore) <= 24*time.Hour && !now.Before(c.NotBefore) && now.Before(c.NotAfter) && c.VerifyHostname("nagi-rewrite-child.internal") == nil && c.CheckSignature(c.SignatureAlgorithm, c.RawTBSCertificate, c.Signature) == nil
}
func decodeRewritePrepare(data []byte) (rewritePrepare, error) {
	var v rewritePrepare
	keys := []string{"version", "generation", "previous_generation", "owner_uid", "handoff_addr", "child_transport_cert", "handoff_secret", "reinjection_secret", "intercept_domains", "target_domains", "rule_version", "block_authorized_quic"}
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return v, errRewritePolicy
	}
	if raw["capture"] != nil {
		keys = append(keys, "capture")
	}
	_, err := rewriteObject(data, keys...)
	if err != nil || json.Unmarshal(data, &v) != nil {
		return v, errRewritePolicy
	}
	host, port, e := net.SplitHostPort(v.HandoffAddr)
	n, pe := strconv.ParseUint(port, 10, 16)
	if v.Version != control.RewriteProtocolVersion || !rewriteHex(v.Generation, 32) || v.PreviousGeneration != nil && !rewriteHex(*v.PreviousGeneration, 32) || v.OwnerUID == 0 || v.OwnerUID == ^uint32(0) || e != nil || host != "127.0.0.1" || pe != nil || n == 0 || strconv.FormatUint(n, 10) != port || !rewriteSecret(v.HandoffSecret) || !rewriteSecret(v.ReinjectionSecret) || v.HandoffSecret == v.ReinjectionSecret || !strings.HasPrefix(v.RuleVersion, "sha256:") || !rewriteHex(strings.TrimPrefix(v.RuleVersion, "sha256:"), 64) || !v.BlockAuthorizedQUIC || !rewriteCertificateValid(v.ChildTransportCert) {
		return v, errRewritePolicy
	}
	if v.Capture != nil && validateCapturePolicy(raw["capture"]) != nil {
		return v, errRewritePolicy
	}
	var errDomains error
	v.InterceptDomains, errDomains = rewriteDomainPatterns(v.InterceptDomains, v.Capture != nil)
	if errDomains != nil {
		return v, errDomains
	}
	v.TargetDomains, errDomains = rewriteDomainPatterns(v.TargetDomains, true)
	if errDomains != nil {
		return v, errDomains
	}
	for _, pattern := range v.InterceptDomains {
		if i := sort.SearchStrings(v.TargetDomains, pattern); i == len(v.TargetDomains) || v.TargetDomains[i] != pattern {
			return v, errRewritePolicy
		}
	}
	return v, nil
}
func rewriteDomainPatterns(values []string, allowEmpty bool) ([]string, error) {
	if values == nil || len(values) > 256 || (!allowEmpty && len(values) == 0) {
		return nil, errRewritePolicy
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, s := range values {
		wild := strings.HasPrefix(s, "*.")
		host := strings.TrimPrefix(s, "*.")
		normalized, e := requestrewrite.NormalizeDomain(host)
		if e != nil {
			return nil, errRewritePolicy
		}
		if wild {
			suffix, _ := publicsuffix.PublicSuffix(normalized)
			if suffix == normalized {
				return nil, errRewritePolicy
			}
			normalized = "*." + normalized
		}
		if seen[normalized] {
			return nil, errRewritePolicy
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out, nil
}
func isRewritePath(path string) bool {
	return path == "/request-rewrite" || strings.HasPrefix(path, "/request-rewrite/")
}
func validateRewriteControl(req privileged.Request, u *url.URL) error {
	// Accept canonical paths and query strings only, including no dangling '?'.
	if req.Path != u.Path && !(req.Method == "DELETE" && req.Path == u.Path+"?"+u.RawQuery) {
		return errRewritePolicy
	}
	switch {
	case req.Method == "GET" && (u.Path == "/request-rewrite/capabilities" || u.Path == "/request-rewrite/status"):
		if u.RawQuery == "" && len(req.Body) == 0 {
			return nil
		}
	case req.Method == "DELETE" && u.Path == "/request-rewrite/session":
		q, e := url.ParseQuery(u.RawQuery)
		if e == nil && len(req.Body) == 0 && len(q) == 1 && len(q["generation"]) == 1 && rewriteHex(q.Get("generation"), 32) && u.RawQuery == "generation="+q.Get("generation") {
			return nil
		}
	case req.Method == "PUT" && u.Path == "/request-rewrite/capture" && u.RawQuery == "":
		return validateCaptureUpdate(req.Body)
	case req.Method == "PUT" && u.Path == "/request-rewrite/session" && u.RawQuery == "":
		_, e := decodeRewritePrepare(req.Body)
		return e
	case (req.Method == "POST" && u.Path == "/request-rewrite/probe" || req.Method == "PUT" && u.Path == "/request-rewrite/branch") && u.RawQuery == "":
		keys := []string{"version", "generation"}
		if req.Method == "PUT" {
			keys = append(keys, "enabled")
		}
		if _, e := rewriteObject(req.Body, keys...); e != nil {
			return e
		}
		var v struct {
			Version    int    `json:"version"`
			Generation string `json:"generation"`
			Enabled    bool   `json:"enabled"`
		}
		if json.Unmarshal(req.Body, &v) == nil && v.Version == control.RewriteProtocolVersion && rewriteHex(v.Generation, 32) {
			return nil
		}
	}
	return errRewritePolicy
}
func (h *RuntimeHandler) validateRewriteOwner(peer int, req privileged.Request) error {
	u, e := url.ParseRequestURI(req.Path)
	if e != nil || !isRewritePath(u.Path) {
		return nil
	}
	if h.SessionUID <= 0 || uint64(h.SessionUID) >= uint64(^uint32(0)) || peer != h.SessionUID {
		return errRewritePolicy
	}
	if req.Method == "PUT" && u.Path == "/request-rewrite/session" {
		if !h.rewriteOwnerBound {
			return errors.New("rewrite_capability_missing: install the matching mihomo engine with request rewrite owner binding support")
		}
		v, e := decodeRewritePrepare(req.Body)
		if e != nil || v.OwnerUID != uint32(h.SessionUID) {
			return errRewritePolicy
		}
	}
	return nil
}

// Only the authenticated prepare response may carry the public transport
// certificate and reinjection endpoint. Status and ordinary error responses
// must never acquire credentials or private metadata as the engine evolves.
func filterRewriteResponse(req privileged.Request, response privileged.Response) (privileged.Response, error) {
	u, e := url.ParseRequestURI(req.Path)
	if e != nil || !isRewritePath(u.Path) {
		return response, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(response.Body, &fields) != nil || fields == nil {
		return privileged.Response{}, errRewritePolicy
	}
	var keys []string
	if !response.OK {
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(response.Body, &envelope)
		code := envelope.Error.Code
		switch code {
		case "rewrite_invalid", "rewrite_policy", "rewrite_generation", "rewrite_limit", "rewrite_tun", "rewrite_auth", "rewrite_unavailable":
		default:
			code = "rewrite_unavailable"
		}
		body, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code, "message": "Request rewrite operation rejected"}})
		response.Body = body
		return response, nil
	}
	switch {
	case req.Method == "GET" && u.Path == "/request-rewrite/capabilities":
		keys = []string{"versions", "tun_only", "original_context", "reinjection", "quic_domain_block", "max_contexts", "max_streams_per_context", "context_idle_seconds", "context_max_seconds", "capture_protocol"}
	case req.Method == "PUT" && u.Path == "/request-rewrite/session":
		keys = []string{"version", "generation", "engine_epoch", "reinjection_addr", "engine_transport_cert", "state"}
	case req.Method == "POST":
		keys = []string{"ready"}
	case req.Method == "DELETE":
		keys = []string{"closed"}
	default:
		keys = []string{"version", "generation", "engine_epoch", "state", "branch_enabled", "tun_running", "rule_version", "contexts", "streams", "last_error", "prepared_generation", "capture"}
	}
	out := map[string]json.RawMessage{}
	for _, key := range keys {
		if value, ok := fields[key]; ok {
			out[key] = value
		}
	}
	response.Body, e = json.Marshal(out)
	return response, e
}

// Inspect only trusted executable help, without configuration, credentials or
// inherited environment. Old engines retain ordinary helper/TUN support.
type rewriteHelpBuffer struct{ buffer bytes.Buffer }

func (b *rewriteHelpBuffer) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > 64<<10 {
		return 0, errors.New("engine help output exceeds bound")
	}
	return b.buffer.Write(p)
}
func rewriteOwnerBootstrapSupported(ctx context.Context, binary, private string) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-h")
	cmd.Dir = private
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + private, "TMPDIR=" + private}
	var output rewriteHelpBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if cmd.Run() != nil {
		return false
	}
	for _, line := range strings.Split(output.buffer.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && (fields[0] == "-request-rewrite-owner-uid" || fields[0] == "--request-rewrite-owner-uid") {
			return true
		}
	}
	return false
}

func validateCapturePolicy(data []byte) error {
	fields, e := rewriteObject(data, "session_id", "revision", "state", "scope")
	if e != nil {
		return e
	}
	scope, e := rewriteObject(fields["scope"], "domains", "process_paths", "app_paths")
	if e != nil {
		return e
	}
	var p capture.Policy
	if json.Unmarshal(data, &p) != nil || !captureSessionID(p.SessionID) || p.Revision == 0 || (p.State != "recording" && p.State != "paused" && p.State != "stopped") {
		return errRewritePolicy
	}
	if _, e = rewriteDomainPatterns(p.Scope.Domains, true); e != nil {
		return e
	}
	for _, key := range []string{"process_paths", "app_paths"} {
		var paths []string
		if json.Unmarshal(scope[key], &paths) != nil || paths == nil || len(paths) > 256 {
			return errRewritePolicy
		}
		seen := map[string]bool{}
		for _, p := range paths {
			if !filepath.IsAbs(p) || filepath.Clean(p) != p || len(p) > 4096 || strings.ContainsAny(p, "\x00\r\n") || seen[p] {
				return errRewritePolicy
			}
			if key == "app_paths" && (!strings.HasSuffix(p, ".app") || strings.Contains(p, ".app/")) {
				return errRewritePolicy
			}
			seen[p] = true
		}
	}
	return nil
}
func captureSessionID(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func validateCaptureUpdate(data []byte) error {
	fields, e := rewriteObject(data, "version", "generation", "previous_revision", "capture")
	if e != nil {
		return e
	}
	var p control.RewriteCapture
	if json.Unmarshal(data, &p) != nil || p.Version != 2 || !rewriteHex(p.Generation, 32) || p.PreviousRevision == ^uint64(0) || p.Capture.Revision != p.PreviousRevision+1 {
		return errRewritePolicy
	}
	return validateCapturePolicy(fields["capture"])
}
