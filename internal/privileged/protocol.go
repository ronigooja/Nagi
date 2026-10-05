// Package privileged defines the small, platform-neutral protocol used by
// Nagi to ask a privileged service to manage the mihomo process.
//
// Validation here is only a first boundary. The server must obtain PeerUID
// from the kernel, bind each operation to that peer's session, and open or
// create filesystem objects without symlink races. A root mihomo controller
// must never be exposed directly to a user: its configuration API can reload
// arbitrary paths. The service also needs to validate the effective mihomo
// configuration before launching it with elevated privileges.
package privileged

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const ProtocolVersion = 1
const MaxMessageBytes = 8 << 20

// SocketPath is the authenticated session endpoint created by the root helper.
func SocketPath(uid int) string {
	return filepath.Join(RuntimeRoot(), "uid-"+strconv.Itoa(uid), "helper.sock")
}

func RuntimeRoot() string {
	if runtime.GOOS == "darwin" {
		return "/Library/Application Support/NagiPrivileged"
	}
	return "/run/nagi"
}

type Operation string

const (
	Start   Operation = "start"
	Stop    Operation = "stop"
	Status  Operation = "status"
	Control Operation = "control"
	Logs    Operation = "logs"
)

// Request is one complete helper operation. A request is encoded as one JSON
// object terminated by a newline; this keeps the protocol stream-friendly and
// lets the helper reject oversized or concatenated messages at its boundary.
type Request struct {
	Version    int             `json:"version"`
	Operation  Operation       `json:"operation"`
	Profile    string          `json:"profile,omitempty"`
	BinaryPath string          `json:"binary_path,omitempty"`
	ConfigPath string          `json:"config_path,omitempty"`
	WorkDir    string          `json:"work_dir,omitempty"`
	SocketPath string          `json:"socket_path,omitempty"`
	LogPath    string          `json:"log_path,omitempty"`
	PIDPath    string          `json:"pid_path,omitempty"`
	Method     string          `json:"method,omitempty"`
	Path       string          `json:"path,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
	Lines      int             `json:"lines,omitempty"`
	Offset     int64           `json:"offset,omitempty"`
}

type Response struct {
	Version int             `json:"version"`
	OK      bool            `json:"ok"`
	Error   string          `json:"error,omitempty"`
	PID     int             `json:"pid,omitempty"`
	Running bool            `json:"running,omitempty"`
	Status  int             `json:"status,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
	Offset  int64           `json:"offset,omitempty"`
}

type Policy struct {
	// PeerUID must come from the Unix socket peer credential, never the request.
	PeerUID int
	// AllowedPaths limits paths supplied by start requests. An empty policy
	// rejects all paths, which is safer than accidentally accepting arbitrary
	// root-owned files.
	AllowedPaths []string
}

func (r Request) Validate(p Policy) error {
	if r.Version != ProtocolVersion {
		return fmt.Errorf("unsupported protocol version %d", r.Version)
	}
	if p.PeerUID <= 0 {
		return errors.New("untrusted peer uid")
	}
	switch r.Operation {
	case Start:
		if err := validateProfile(r.Profile); err != nil {
			return err
		}
		if r.Method != "" || r.Path != "" || len(r.Body) != 0 || r.Lines != 0 || r.Offset != 0 {
			return errors.New("unexpected control fields")
		}
		if r.BinaryPath != "" || r.WorkDir != "" || r.SocketPath != "" || r.LogPath != "" || r.PIDPath != "" {
			return errors.New("untrusted engine paths are not allowed")
		}
		if r.ConfigPath == "" {
			return errors.New("configuration path is required")
		}
		if err := validatePath(r.ConfigPath, p.AllowedPaths); err != nil {
			return fmt.Errorf("config_path: %w", err)
		}
	case Stop, Status:
		if r.BinaryPath != "" || r.ConfigPath != "" || r.WorkDir != "" || r.SocketPath != "" || r.LogPath != "" || r.PIDPath != "" || r.Method != "" || r.Path != "" || len(r.Body) != 0 || r.Lines != 0 || r.Offset != 0 {
			return errors.New("unexpected start fields")
		}
	case Control:
		if r.Lines != 0 || r.Offset != 0 {
			return errors.New("unexpected log fields")
		}
		if r.BinaryPath != "" || r.ConfigPath != "" || r.WorkDir != "" || r.SocketPath != "" || r.LogPath != "" || r.PIDPath != "" {
			return errors.New("unexpected start fields")
		}
		if r.Method == "" || r.Path == "" || len(r.Path) > 2048 || len(r.Body) > 1<<20 || len(r.Body) != 0 && !json.Valid(r.Body) {
			return errors.New("invalid control request")
		}
	case Logs:
		if r.BinaryPath != "" || r.ConfigPath != "" || r.WorkDir != "" || r.SocketPath != "" || r.LogPath != "" || r.PIDPath != "" || r.Method != "" || r.Path != "" || len(r.Body) != 0 || r.Lines < 0 || r.Lines > 10000 || r.Offset < -1 {
			return errors.New("invalid log request")
		}
	default:
		return fmt.Errorf("unsupported operation %q", r.Operation)
	}
	return nil
}

func (r Response) Validate() error {
	if r.Version != ProtocolVersion {
		return fmt.Errorf("unsupported protocol version %d", r.Version)
	}
	if r.OK && r.Error != "" {
		return errors.New("successful response cannot contain error")
	}
	if len(r.Body) > MaxMessageBytes-1024 || len(r.Body) != 0 && !json.Valid(r.Body) {
		return errors.New("invalid control response")
	}
	return nil
}

func WriteRequest(w io.Writer, r Request) error   { return writeJSON(w, r) }
func WriteResponse(w io.Writer, r Response) error { return writeJSON(w, r) }

func ReadRequest(r io.Reader) (Request, error) {
	var v Request
	if err := readJSON(r, &v); err != nil {
		return v, err
	}
	return v, nil
}

func ReadResponse(r io.Reader) (Response, error) {
	var v Response
	if err := readJSON(r, &v); err != nil {
		return v, err
	}
	return v, nil
}

func writeJSON(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

func readJSON(r io.Reader, v any) error {
	line, err := bufio.NewReader(io.LimitReader(r, MaxMessageBytes+2)).ReadBytes('\n')
	if err != nil {
		return err
	}
	if len(line) > MaxMessageBytes {
		return errors.New("protocol message too large")
	}
	dec := json.NewDecoder(strings.NewReader(string(line)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("multiple JSON values in protocol message")
	}
	return nil
}

func validatePath(path string, allowed []string) error {
	if !filepath.IsAbs(path) {
		return errors.New("must be absolute")
	}
	clean := filepath.Clean(path)
	if clean != path {
		return errors.New("must be clean")
	}
	if len(allowed) == 0 {
		return errors.New("path is not allowed")
	}
	// A root process must not follow an attacker-controlled symlink from a
	// user-owned directory to a privileged path. Reject every existing
	// component, including the final file, when it is a symlink.
	for current := clean; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink path component is not allowed")
		}
		if parent := filepath.Dir(current); parent == current {
			break
		}
	}
	for _, root := range allowed {
		if !filepath.IsAbs(root) {
			continue
		}
		root = filepath.Clean(root)
		rel, err := filepath.Rel(root, clean)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	return errors.New("outside allowed directory")
}

func validateProfile(profile string) error {
	if profile == "" || len(profile) > 128 {
		return errors.New("invalid profile")
	}
	for _, ch := range profile {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return errors.New("invalid profile")
		}
	}
	return nil
}
