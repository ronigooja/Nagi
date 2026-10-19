// Package requestrewriteservice implements the private rewrite child data plane.
// It never resolves or dials an application destination itself.
package requestrewriteservice

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const childRole = "nagi-rewrite-child.internal"
const engineRole = "nagi-rewrite-engine.internal"
const probeTarget = "nagi-rewrite-probe.invalid:443"
const probePayload = "NAGI-REWRITE-PROBE-2"

var errInvalid = errors.New("rewrite_invalid")
var errAuth = errors.New("rewrite_auth")
var errUnavailable = errors.New("rewrite_unavailable")
var errLimit = errors.New("rewrite_limit")

func randomValue(n int) (string, error) {
	b := make([]byte, n)
	_, e := rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b), e
}
func NewGeneration() (string, error) {
	b := make([]byte, 16)
	_, e := rand.Read(b)
	return hex.EncodeToString(b), e
}
func certificate() (tls.Certificate, error) {
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return tls.Certificate{}, errUnavailable
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return tls.Certificate{}, errUnavailable
	}
	now := time.Now()
	t := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: childRole}, DNSNames: []string{childRole}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(12 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, t, t, &key.PublicKey, key)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, e
}
func parsePeer(encoded string) (*x509.Certificate, error) {
	der, e := base64.StdEncoding.DecodeString(encoded)
	if e != nil {
		return nil, errAuth
	}
	c, e := x509.ParseCertificate(der)
	if e != nil {
		return nil, errAuth
	}
	key, ok := c.PublicKey.(*ecdsa.PublicKey)
	client, server := false, false
	for _, v := range c.ExtKeyUsage {
		client = client || v == x509.ExtKeyUsageClientAuth
		server = server || v == x509.ExtKeyUsageServerAuth
	}
	now := time.Now()
	if !ok || key.Curve != elliptic.P256() || !client || !server || c.NotAfter.Sub(c.NotBefore) > 24*time.Hour || now.Before(c.NotBefore) || !now.Before(c.NotAfter) || c.VerifyHostname(engineRole) != nil || c.CheckSignature(c.SignatureAlgorithm, c.RawTBSCertificate, c.Signature) != nil {
		return nil, errAuth
	}
	return c, nil
}
func channelTLS(local tls.Certificate, peer *x509.Certificate, server bool) *tls.Config {
	roots := x509.NewCertPool()
	roots.AddCert(peer)
	pin := sha256.Sum256(peer.RawSubjectPublicKeyInfo)
	c := &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{local}, RootCAs: roots, ServerName: engineRole, NextProtos: []string{"http/1.1"}}
	if server {
		c.ClientAuth = tls.RequireAndVerifyClientCert
		c.ClientCAs = roots
	}
	c.VerifyConnection = func(s tls.ConnectionState) error {
		if len(s.PeerCertificates) != 1 {
			return errAuth
		}
		p := s.PeerCertificates[0]
		now := time.Now()
		if !bytes.Equal(p.Raw, peer.Raw) || sha256.Sum256(p.RawSubjectPublicKeyInfo) != pin || p.VerifyHostname(engineRole) != nil || now.Before(p.NotBefore) || !now.Before(p.NotAfter) {
			return errAuth
		}
		return nil
	}
	return c
}
func loopback(addr string) bool {
	h, p, e := net.SplitHostPort(addr)
	n, x := strconv.Atoi(p)
	return e == nil && x == nil && h == "127.0.0.1" && n > 0 && n <= 65535 && strconv.Itoa(n) == p
}
func id(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 16 && hex.EncodeToString(b) == s
}
func token(s string) bool {
	b, e := base64.RawURLEncoding.DecodeString(s)
	return e == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == s
}
func bearer(value, secret string) bool {
	return subtle.ConstantTimeCompare([]byte(value), []byte("Bearer "+secret)) == 1
}
func readHead(r *bufio.Reader, limit int) ([]byte, error) {
	b := make([]byte, 0, 512)
	for len(b) < limit {
		v, e := r.ReadByte()
		if e != nil {
			return nil, errUnavailable
		}
		b = append(b, v)
		if bytes.HasSuffix(b, []byte("\r\n\r\n")) {
			return b, nil
		}
	}
	return nil, errLimit
}
func readOK(r *bufio.Reader) error {
	b, e := readHead(r, 16<<10)
	if e != nil {
		return e
	}
	v, e := http.ReadResponse(bufio.NewReader(bytes.NewReader(b)), nil)
	if e != nil || v.StatusCode != 200 {
		return errUnavailable
	}
	return nil
}
func wireError(w io.Writer, code string) {
	body := code + "\n"
	_, _ = fmt.Fprintf(w, "HTTP/1.1 502 Bad Gateway\r\nContent-Type: text/plain\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
}
func headFields(b []byte, allowed map[string]bool) error {
	if !strictCRLF(b) {
		return errInvalid
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(b), "\r\n")[1:] {
		if line == "" {
			break
		}
		i := strings.IndexByte(line, ':')
		if i < 1 || line[0] == ' ' || line[0] == '\t' {
			return errInvalid
		}
		k := strings.ToLower(line[:i])
		if seen[k] || !allowed[k] {
			return errInvalid
		}
		seen[k] = true
	}
	if !seen["host"] {
		return errInvalid
	}
	return nil
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) { return c.r.Read(b) }

type connectionContext struct {
	ctx        context.Context
	cancel     context.CancelFunc
	token      string
	service    *Service
	mu         sync.Mutex // serializes allocation through successful acknowledgement
	open       uint64
	sockets    map[net.Conn]bool
	transports map[string]*http.Transport
}

func (c *connectionContext) close() {
	c.cancel()
	c.mu.Lock()
	ss := make([]net.Conn, 0, len(c.sockets))
	for s := range c.sockets {
		ss = append(ss, s)
	}
	tt := make([]*http.Transport, 0, len(c.transports))
	for _, t := range c.transports {
		tt = append(tt, t)
	}
	c.mu.Unlock()
	for _, t := range tt {
		t.CloseIdleConnections()
	}
	for _, s := range ss {
		_ = s.Close()
	}
}
func (c *connectionContext) dial(ctx context.Context, target string, probe bool) (net.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx.Err() != nil || ctx.Err() != nil {
		return nil, errUnavailable
	}
	if c.open == ^uint64(0) {
		return nil, errLimit
	}
	s := c.service
	cfg := s.config
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	raw, e := (&net.Dialer{}).DialContext(dctx, "tcp4", cfg.ReinjectionAddr)
	if e != nil {
		return nil, errUnavailable
	}
	success := false
	defer func() {
		if !success {
			raw.Close()
		}
	}()
	raw.SetDeadline(deadline)
	conn := tls.Client(raw, channelTLS(s.cert, s.peer, false))
	if conn.HandshakeContext(dctx) != nil {
		return nil, errAuth
	}
	c.open++
	extra := ""
	if probe {
		extra = "Nagi-Rewrite-Probe: 1\r\n"
	}
	_, e = fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nNagi-Rewrite-Version: 2\r\nNagi-Rewrite-Generation: %s\r\nNagi-Rewrite-Epoch: %s\r\nNagi-Rewrite-Context: %s\r\nNagi-Rewrite-Open: %d\r\n%s\r\n", target, target, cfg.ReinjectionSecret, cfg.Generation, cfg.EngineEpoch, c.token, c.open, extra)
	if e != nil {
		c.cancel()
		return nil, errUnavailable
	}
	reader := bufio.NewReader(conn)
	if e = readOK(reader); e != nil {
		c.cancel()
		return nil, e
	}
	conn.SetDeadline(time.Time{})
	wrapped := &trackedConn{Conn: &bufferedConn{conn, reader}, owner: c}
	c.sockets[wrapped] = true
	success = true
	return wrapped, nil
}

type trackedConn struct {
	net.Conn
	owner *connectionContext
	once  sync.Once
}

func (c *trackedConn) Close() error {
	e := c.Conn.Close()
	c.once.Do(func() { c.owner.mu.Lock(); delete(c.owner.sockets, c); c.owner.mu.Unlock() })
	return e
}

func strictCRLF(b []byte) bool {
	for i, c := range b {
		if c == '\n' && (i == 0 || b[i-1] != '\r') {
			return false
		}
		if c == '\r' && (i+1 == len(b) || b[i+1] != '\n') {
			return false
		}
	}
	return true
}
