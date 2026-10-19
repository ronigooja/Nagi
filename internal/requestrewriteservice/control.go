package requestrewriteservice

import (
	"context"
	"encoding/json"
	"github.com/ronigooja/Nagi/internal/capture"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type Status struct {
	Capture     *capture.Policy `json:"capture,omitempty"`
	Version     int             `json:"version"`
	Generation  string          `json:"generation"`
	RuleVersion string          `json:"rule_version"`
	Ready       bool            `json:"ready"`
	Contexts    int             `json:"contexts"`
	LastError   *string         `json:"last_error"`
	Counters    RewriteCounters `json:"counters"`
}

func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	var policy *capture.Policy
	if s.capturePolicy != nil {
		p := *s.capturePolicy
		p.Scope.Domains = append([]string{}, p.Scope.Domains...)
		p.Scope.ProcessPaths = append([]string{}, p.Scope.ProcessPaths...)
		p.Scope.AppPaths = append([]string{}, p.Scope.AppPaths...)
		policy = &p
	}
	return Status{Capture: policy, Version: 2, Generation: s.config.Generation, RuleVersion: s.snapshot.Version(), Ready: s.configured && !s.closing && !s.draining, Contexts: len(s.contexts), Counters: s.counters}
}

type peerKey struct{}

// ListenControl binds only inside an already private owner directory. A held
// no-follow flock and refusal to replace sockets prevent duplicate ownership.
func (s *Service) ListenControl(runtimeDir string, ownerUID int) (string, error) {
	if ownerUID != os.Geteuid() || !filepath.IsAbs(runtimeDir) || filepath.Clean(runtimeDir) != runtimeDir {
		return "", errAuth
	}
	// Walk through no-follow directory descriptors. Ancestors must be root or
	// owner controlled and not writable by other identities; root sticky temp
	// directories are safe because another UID cannot rename our owned child.
	fd, e := unix.Open("/", unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return "", errAuth
	}
	parts := strings.Split(strings.TrimPrefix(runtimeDir, "/"), "/")
	var st unix.Stat_t
	for i, part := range parts {
		next, e := unix.Openat(fd, part, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if e != nil {
			return "", errAuth
		}
		fd = next
		if unix.Fstat(fd, &st) != nil {
			unix.Close(fd)
			return "", errAuth
		}
		final := i == len(parts)-1
		if final {
			if int(st.Uid) != ownerUID || st.Mode&077 != 0 {
				unix.Close(fd)
				return "", errAuth
			}
		} else {
			if int(st.Uid) != ownerUID && st.Uid != 0 || st.Mode&022 != 0 && !(st.Uid == 0 && st.Mode&unix.S_ISVTX != 0) {
				unix.Close(fd)
				return "", errAuth
			}
		}
	}
	defer unix.Close(fd)
	name := "rewrite-" + s.config.Generation
	lockfd, e := unix.Openat(fd, name+".lock", unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_RDWR, 0600)
	if e != nil {
		return "", errAuth
	}
	lock := os.NewFile(uintptr(lockfd), "rewrite-lock")
	if unix.Fstat(lockfd, &st) != nil || int(st.Uid) != ownerUID || st.Mode&077 != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || unix.Flock(lockfd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		lock.Close()
		return "", errAuth
	}
	socket := filepath.Join(runtimeDir, name+".sock")
	if _, e = os.Lstat(socket); !os.IsNotExist(e) {
		lock.Close()
		return "", errAuth
	}
	listener, e := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if e != nil {
		lock.Close()
		return "", errUnavailable
	}
	if os.Chmod(socket, 0600) != nil {
		listener.Close()
		lock.Close()
		return "", errAuth
	}
	s.mu.Lock()
	if s.closing || s.control != nil {
		s.mu.Unlock()
		listener.Close()
		lock.Close()
		return "", errInvalid
	}
	s.control = listener
	s.controlLock = lock
	s.workers.Add(1)
	s.mu.Unlock()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, ok := r.Context().Value(peerKey{}).(int)
		if !ok || uid != ownerUID {
			http.Error(w, "rewrite_auth", 403)
			return
		}
		if r.URL.RawQuery != "" {
			http.Error(w, "rewrite_invalid", 400)
			return
		}
		shutdown := false
		switch {
		case r.Method == "GET" && r.URL.Path == "/status":
			if r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
				http.Error(w, "rewrite_invalid", 400)
				return
			}
		case r.Method == "PUT" && r.URL.Path == "/capture":
			b, e := io.ReadAll(io.LimitReader(r.Body, 65537))
			var update struct {
				Version    int            `json:"version"`
				Generation string         `json:"generation"`
				Capture    capture.Policy `json:"capture"`
			}
			if e != nil || len(b) > 65536 || !uniqueJSON(b) || !exactKeys(b, reflect.TypeOf(update)) || json.Unmarshal(b, &update) != nil || update.Version != 2 || update.Generation != s.config.Generation {
				http.Error(w, "rewrite_invalid", 409)
				return
			}
			var raw map[string]json.RawMessage
			if json.Unmarshal(b, &raw) != nil || len(raw) != 3 || raw["version"] == nil || raw["generation"] == nil || raw["capture"] == nil || s.SetCapture(update.Capture) != nil {
				http.Error(w, "rewrite_invalid", 409)
				return
			}
		case r.Method == "POST" && (r.URL.Path == "/drain" || r.URL.Path == "/shutdown"):
			b, e := io.ReadAll(io.LimitReader(r.Body, 4097))
			if e != nil || len(b) > 4096 || !uniqueJSON(b) {
				http.Error(w, "rewrite_invalid", 400)
				return
			}
			var v map[string]json.RawMessage
			if json.Unmarshal(b, &v) != nil || len(v) != 2 || v["version"] == nil || v["generation"] == nil {
				http.Error(w, "rewrite_invalid", 400)
				return
			}
			var version int
			var generation string
			if json.Unmarshal(v["version"], &version) != nil || json.Unmarshal(v["generation"], &generation) != nil || version != 2 || generation != s.config.Generation {
				http.Error(w, "rewrite_invalid", 409)
				return
			}
			s.mu.Lock()
			s.draining = true
			s.mu.Unlock()
			shutdown = r.URL.Path == "/shutdown"
		default:
			http.Error(w, "rewrite_invalid", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.Status())
		if shutdown {
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			go s.Close()
		}
	}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 4096, ErrorLog: log.New(io.Discard, "", 0), ConnContext: func(ctx context.Context, c net.Conn) context.Context {
		uid, e := peerUID(c)
		if e != nil {
			uid = -1
		}
		return context.WithValue(ctx, peerKey{}, uid)
	}}
	go func() { defer s.workers.Done(); _ = server.Serve(listener) }()
	return socket, nil
}
func Control(ctx context.Context, socket, method, path, generation string) (Status, error) {
	if method != "GET" && method != "POST" || path != "/status" && path != "/drain" && path != "/shutdown" {
		return Status{}, errInvalid
	}
	body := ""
	if method == "POST" {
		if !id(generation) {
			return Status{}, errInvalid
		}
		body = `{"version":2,"generation":` + strconv.Quote(generation) + `}`
	}
	return controlBody(ctx, socket, method, path, body)
}
func ControlCapture(ctx context.Context, socket, generation string, policy capture.Policy) (Status, error) {
	b, e := json.Marshal(struct {
		Version    int            `json:"version"`
		Generation string         `json:"generation"`
		Capture    capture.Policy `json:"capture"`
	}{2, generation, policy})
	if e != nil {
		return Status{}, errInvalid
	}
	return controlBody(ctx, socket, "PUT", "/capture", string(b))
}
func controlBody(ctx context.Context, socket, method, path, body string) (Status, error) {
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		conn, e := (&net.Dialer{}).DialContext(ctx, "unix", socket)
		if e != nil {
			return nil, e
		}
		uid, e := peerUID(conn)
		if e != nil || uid != os.Geteuid() {
			conn.Close()
			return nil, errAuth
		}
		return conn, nil
	}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	req, e := http.NewRequestWithContext(ctx, method, "http://rewrite.internal"+path, strings.NewReader(body))
	if e != nil {
		return Status{}, errInvalid
	}
	response, e := transport.RoundTrip(req)
	if e != nil {
		return Status{}, errUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return Status{}, errUnavailable
	}
	var result Status
	if json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result) != nil {
		return Status{}, errInvalid
	}
	return result, nil
}
