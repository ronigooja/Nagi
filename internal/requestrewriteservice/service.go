package requestrewriteservice

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	domain "github.com/ronigooja/Nagi/internal/requestrewrite"
)

// Configuration is supplied only through the child's inherited control pipe.
// Its secrets and endpoints must never enter ordinary output or diagnostics.
type Configuration struct {
	Generation          string `json:"generation"`
	EngineEpoch         string `json:"engine_epoch"`
	ReinjectionAddr     string `json:"reinjection_addr"`
	EngineTransportCert string `json:"engine_transport_cert"`
	HandoffSecret       string `json:"handoff_secret"`
	ReinjectionSecret   string `json:"reinjection_secret"`
}
type Announcement struct {
	HandoffAddr        string `json:"handoff_addr"`
	ChildTransportCert string `json:"child_transport_cert"`
	RuleVersion        string `json:"rule_version"`
}
type Service struct {
	transformTimeout time.Duration
	control          net.Listener
	controlServer    *http.Server
	controlLock      *os.File
	draining         bool
	snapshot         *domain.Snapshot
	ca               tls.Certificate
	cert             tls.Certificate
	peer             *x509.Certificate
	config           Configuration
	listener         net.Listener
	ctx              context.Context
	cancel           context.CancelFunc
	mu               sync.Mutex
	contexts         map[string]*connectionContext
	connections      map[net.Conn]bool
	pending          chan struct{}
	workers          sync.WaitGroup
	configured       bool
	closing          bool
	leaves           *certCache
	originRoots      *x509.CertPool
}

// New opens an ephemeral loopback listener, without starting admission. Only the
// unprivileged child calls CAStore.LoadForChild before passing its certificate.
func New(snapshot *domain.Snapshot, ca tls.Certificate) (*Service, error) {
	if snapshot == nil || len(ca.Certificate) == 0 || ca.PrivateKey == nil {
		return nil, errInvalid
	}
	cert, e := certificate()
	if e != nil {
		return nil, errUnavailable
	}
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return nil, errUnavailable
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{snapshot: snapshot, transformTimeout: 30 * time.Second, ca: ca, cert: cert, listener: l, ctx: ctx, cancel: cancel, contexts: map[string]*connectionContext{}, connections: map[net.Conn]bool{}, pending: make(chan struct{}, 32), leaves: &certCache{entries: map[string]*tls.Certificate{}}}, nil
}
func (s *Service) Announcement() Announcement {
	return Announcement{s.listener.Addr().String(), base64.StdEncoding.EncodeToString(s.cert.Certificate[0]), s.snapshot.Version()}
}
func (s *Service) Configure(c Configuration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.configured || !id(c.Generation) || !id(c.EngineEpoch) || !loopback(c.ReinjectionAddr) || !token(c.HandoffSecret) || !token(c.ReinjectionSecret) || c.HandoffSecret == c.ReinjectionSecret {
		return errInvalid
	}
	p, e := parsePeer(c.EngineTransportCert)
	if e != nil {
		return e
	}
	s.config = c
	s.peer = p
	s.configured = true
	return nil
}
func (s *Service) Serve() error {
	s.mu.Lock()
	ready := s.configured && !s.closing
	if ready {
		s.workers.Add(1)
	}
	s.mu.Unlock()
	if !ready {
		return errInvalid
	}
	go func() { defer s.workers.Done(); s.heartbeat() }()
	for {
		raw, e := s.listener.Accept()
		if e != nil {
			if s.ctx.Err() != nil {
				return nil
			}
			return errUnavailable
		}
		select {
		case s.pending <- struct{}{}:
		default:
			raw.Close()
			continue
		}
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			raw.Close()
			<-s.pending
			return nil
		}
		s.connections[raw] = true
		s.workers.Add(1)
		s.mu.Unlock()
		go func() { defer s.workers.Done(); s.receive(raw) }()
	}
}
func (s *Service) Close() error {
	s.mu.Lock()
	s.closing = true
	s.cancel()
	s.listener.Close()
	if s.control != nil {
		s.control.Close()
	}
	if s.controlServer != nil {
		s.controlServer.Close()
	}
	ss := make([]net.Conn, 0, len(s.connections))
	for c := range s.connections {
		ss = append(ss, c)
	}
	cc := make([]*connectionContext, 0, len(s.contexts))
	for _, c := range s.contexts {
		cc = append(cc, c)
	}
	s.mu.Unlock()
	for _, c := range ss {
		c.Close()
	}
	for _, c := range cc {
		c.close()
	}
	s.workers.Wait()
	if s.controlLock != nil {
		s.controlLock.Close()
	}
	s.mu.Lock()
	s.config.HandoffSecret = ""
	s.config.ReinjectionSecret = ""
	s.mu.Unlock()
	return nil
}
func (s *Service) receive(raw net.Conn) {
	pending := true
	defer func() {
		if pending {
			<-s.pending
		}
		raw.Close()
		s.mu.Lock()
		delete(s.connections, raw)
		s.mu.Unlock()
	}()
	addr, ok := raw.RemoteAddr().(*net.TCPAddr)
	if !ok || !addr.IP.IsLoopback() {
		return
	}
	raw.SetDeadline(time.Now().Add(5 * time.Second))
	conn := tls.Server(raw, channelTLS(s.cert, s.peer, true))
	if conn.HandshakeContext(s.ctx) != nil {
		return
	}
	r := bufio.NewReader(conn)
	head, e := readHead(r, 16<<10)
	if e != nil {
		return
	}
	allowed := map[string]bool{"host": true, "authorization": true, "nagi-rewrite-version": true, "nagi-rewrite-generation": true, "nagi-rewrite-epoch": true, "nagi-rewrite-context": true, "nagi-rewrite-protocol": true, "nagi-rewrite-probe": true}
	if headFields(head, allowed) != nil {
		wireError(conn, "rewrite_invalid")
		return
	}
	req, e := http.ReadRequest(bufio.NewReader(bytes.NewReader(head)))
	if e != nil {
		wireError(conn, "rewrite_invalid")
		return
	}
	h := req.Header
	c := s.config
	if !bearer(h.Get("Authorization"), c.HandoffSecret) {
		wireError(conn, "rewrite_auth")
		return
	}
	protocol := h.Get("Nagi-Rewrite-Protocol")
	probe := h.Get("Nagi-Rewrite-Probe")
	if _, present := h["Nagi-Rewrite-Probe"]; present && probe != "1" {
		wireError(conn, "rewrite_invalid")
		return
	}
	if req.Method != "CONNECT" || req.Proto != "HTTP/1.1" || req.Host != req.RequestURI || req.ContentLength > 0 || len(req.TransferEncoding) > 0 || h.Get("Nagi-Rewrite-Version") != "1" || h.Get("Nagi-Rewrite-Generation") != c.Generation || h.Get("Nagi-Rewrite-Epoch") != c.EngineEpoch || !token(h.Get("Nagi-Rewrite-Context")) || (protocol != "http" && protocol != "tls") || (probe != "" && probe != "1") {
		wireError(conn, "rewrite_invalid")
		return
	}
	if probe == "1" && (req.Host != probeTarget || protocol != "http") {
		wireError(conn, "rewrite_auth")
		return
	}
	host, port, e := net.SplitHostPort(req.Host)
	pn, pe := strconv.Atoi(port)
	rawHost := ""
	for _, line := range strings.Split(string(head), "\r\n")[1:] {
		if strings.HasPrefix(strings.ToLower(line), "host:") {
			rawHost = strings.TrimSpace(line[5:])
		}
	}
	if e != nil || pe != nil || pn < 1 || pn > 65535 || strconv.Itoa(pn) != port || rawHost != req.RequestURI || probe == "" && !s.snapshot.Authorized(host) {
		wireError(conn, "rewrite_auth")
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, time.Hour)
	cc := &connectionContext{ctx: ctx, cancel: cancel, token: h.Get("Nagi-Rewrite-Context"), service: s, sockets: map[net.Conn]bool{}, transports: map[string]*http.Transport{}}
	s.mu.Lock()
	if s.draining || s.closing || len(s.contexts) >= 1024 || s.contexts[cc.token] != nil {
		s.mu.Unlock()
		cancel()
		wireError(conn, "rewrite_limit")
		return
	}
	s.contexts[cc.token] = cc
	s.mu.Unlock()
	defer func() { cc.close(); s.mu.Lock(); delete(s.contexts, cc.token); s.mu.Unlock() }()
	<-s.pending
	pending = false
	_, e = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
	if e != nil {
		return
	}
	conn.SetDeadline(time.Time{})
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	if probe == "1" {
		up, e := cc.dial(ctx, probeTarget, true)
		if e != nil {
			return
		}
		defer up.Close()
		up.SetDeadline(time.Now().Add(5 * time.Second))
		incoming := make([]byte, len(probePayload))
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, e = io.ReadFull(r, incoming); e != nil || string(incoming) != probePayload {
			return
		}
		if _, e = io.WriteString(up, probePayload); e != nil {
			return
		}
		b := make([]byte, len(probePayload))
		if _, e = io.ReadFull(up, b); e != nil || string(b) != probePayload {
			return
		}
		conn.Write(b)
		return
	}
	s.application(cc, &bufferedConn{conn, r}, req.Host, protocol)
}
func (s *Service) heartbeat() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var sequence uint64
	send := func() { sequence++; s.sendHeartbeat(sequence) }
	send()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			send()
		}
	}
}
func (s *Service) sendHeartbeat(sequence uint64) {
	ctx, cancel := context.WithTimeout(s.ctx, time.Second)
	defer cancel()
	c := s.config
	raw, e := (&net.Dialer{}).DialContext(ctx, "tcp4", c.ReinjectionAddr)
	if e != nil {
		return
	}
	defer raw.Close()
	raw.SetDeadline(time.Now().Add(time.Second))
	conn := tls.Client(raw, channelTLS(s.cert, s.peer, false))
	if conn.HandshakeContext(ctx) != nil {
		return
	}
	body, _ := json.Marshal(struct {
		Version     int    `json:"version"`
		Generation  string `json:"generation"`
		Epoch       string `json:"engine_epoch"`
		RuleVersion string `json:"rule_version"`
		Ready       bool   `json:"ready"`
		Sequence    uint64 `json:"sequence"`
	}{1, c.Generation, c.EngineEpoch, s.snapshot.Version(), s.Status().Ready, sequence})
	_, _ = fmt.Fprintf(conn, "POST /_nagi/request-rewrite/heartbeat HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", c.ReinjectionAddr, c.ReinjectionSecret, len(body), body)
	r := bufio.NewReader(conn)
	b, e := readHead(r, 16<<10)
	if e != nil {
		return
	}
	resp, e := http.ReadResponse(bufio.NewReader(bytes.NewReader(b)), nil)
	if e != nil || resp.StatusCode != 200 {
		return
	}
	_, _ = io.CopyN(io.Discard, r, resp.ContentLength)
}
func stripHeaders(h http.Header) {
	for k := range h {
		if strings.HasPrefix(strings.ToLower(k), "nagi-rewrite-") {
			delete(h, k)
		}
	}
	for _, v := range h.Values("Connection") {
		for _, name := range strings.Split(v, ",") {
			if !strings.EqualFold(strings.TrimSpace(name), "upgrade") {
				h.Del(strings.TrimSpace(name))
			}
		}
	}
	for _, k := range []string{"Proxy-Authorization", "Proxy-Authenticate", "Proxy-Connection", "Keep-Alive", "Te"} {
		h.Del(k)
	}
}
