package requestrewriteservice

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"time"

	domain "github.com/ronigooja/Nagi/internal/requestrewrite"
)

// InternalArgument is intentionally absent from public CLI help/completion.
const InternalArgument = "__nagi-request-rewrite-child"
const frameLimit = 2 << 20

type startup struct {
	RuntimeDir string          `json:"runtime_dir"`
	Generation string          `json:"generation"`
	OwnerUID   int             `json:"owner_uid"`
	Version    int             `json:"version"`
	CADir      string          `json:"ca_dir"`
	Document   json.RawMessage `json:"document"`
}
type childCommand struct {
	Configure *Configuration `json:"configure,omitempty"`
	Stop      bool           `json:"stop,omitempty"`
}
type childReply struct {
	Announcement *Announcement `json:"announcement,omitempty"`
	Ready        bool          `json:"ready,omitempty"`
	Error        string        `json:"error,omitempty"`
}

func writeFrame(w io.Writer, v any) error {
	b, e := json.Marshal(v)
	if e != nil || len(b) > frameLimit {
		return errInvalid
	}
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(b)))
	if _, e = w.Write(n[:]); e != nil {
		return errUnavailable
	}
	if _, e = w.Write(b); e != nil {
		return errUnavailable
	}
	return nil
}
func readFrame(r io.Reader, v any) error {
	var n [4]byte
	if _, e := io.ReadFull(r, n[:]); e != nil {
		return errUnavailable
	}
	size := binary.BigEndian.Uint32(n[:])
	if size == 0 || size > frameLimit {
		return errLimit
	}
	b := make([]byte, int(size))
	if _, e := io.ReadFull(r, b); e != nil {
		return errUnavailable
	}
	if !uniqueJSON(b) || !exactKeys(b, reflect.TypeOf(v).Elem()) {
		return errInvalid
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return errInvalid
	}
	return nil
}
func uniqueJSON(b []byte) bool {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var walk func(int) bool
	walk = func(depth int) bool {
		if depth > 260 {
			return false
		}
		t, e := d.Token()
		if e != nil {
			return false
		}
		if x, ok := t.(json.Delim); ok {
			switch x {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					s, ok := k.(string)
					if e != nil || !ok || seen[s] {
						return false
					}
					seen[s] = true
					if !walk(depth + 1) {
						return false
					}
				}
			case '[':
				for d.More() {
					if !walk(depth + 1) {
						return false
					}
				}
			default:
				return false
			}
			_, e = d.Token()
			return e == nil
		}
		return true
	}
	if !walk(0) {
		return false
	}
	_, e := d.Token()
	return e == io.EOF
}

// RunChild serves only inherited fd 3/4. It refuses root before touching CA
// paths. Startup descriptors are consumed and closed; subsequent supervision
// uses the private owner-authenticated Unix control socket.
func RunChild() int {
	if os.Geteuid() == 0 {
		return 1
	}
	input := os.NewFile(3, "rewrite-control-input")
	output := os.NewFile(4, "rewrite-control-output")
	if input == nil || output == nil {
		return 1
	}
	defer input.Close()
	defer output.Close()
	input.SetReadDeadline(time.Now().Add(5 * time.Second))
	var init startup
	if readFrame(input, &init) != nil || init.Version != 2 || init.OwnerUID != os.Geteuid() || !id(init.Generation) {
		return 1
	}
	snapshot, e := domain.Parse(init.Document)
	if e != nil {
		return 1
	}
	cert, key, e := (domain.CAStore{ConfigDir: init.CADir}).LoadForChild(time.Now())
	if e != nil {
		return 1
	}
	service, e := New(snapshot, tls.Certificate{Certificate: [][]byte{cert.Raw}, PrivateKey: key, Leaf: cert})
	if e != nil {
		return 1
	}
	defer service.Close()
	a := service.Announcement()
	if writeFrame(output, childReply{Announcement: &a}) != nil {
		return 1
	}
	input.SetReadDeadline(time.Now().Add(30 * time.Second))
	var command childCommand
	if readFrame(input, &command) != nil || command.Configure == nil || command.Stop {
		return 1
	}
	if command.Configure.Generation != init.Generation || service.Configure(*command.Configure) != nil {
		return 1
	}
	if _, e = service.ListenControl(init.RuntimeDir, init.OwnerUID); e != nil {
		return 1
	}
	input.Close()
	done := make(chan error, 1)
	go func() { done <- service.Serve() }()
	if writeFrame(output, childReply{Ready: true}) != nil {
		return 1
	}
	output.Close()
	if e := <-done; e != nil {
		return 1
	}
	return 0
}

// Child is an internal process handle for the T5 coordinator. Start/Configure
// stages one immutable generation; it performs no engine lifecycle operations.
type Child struct {
	Announcement Announcement
	socket       string
	generation   string
	cmd          *exec.Cmd
	input        *os.File
	output       *os.File
	mu           sync.Mutex
	done         chan error
	once         sync.Once
}

type ChildOptions struct {
	CADir      string
	RuntimeDir string
	Generation string
	Document   []byte
}

func StartChild(ctx context.Context, executable string, options ChildOptions) (*Child, error) {
	if os.Geteuid() == 0 {
		return nil, errAuth
	}
	if ctx.Err() != nil || !id(options.Generation) {
		return nil, errInvalid
	}
	if _, e := domain.Parse(options.Document); e != nil {
		return nil, errInvalid
	}
	childIn, parentIn, e := os.Pipe()
	if e != nil {
		return nil, errUnavailable
	}
	parentOut, childOut, e := os.Pipe()
	if e != nil {
		childIn.Close()
		parentIn.Close()
		return nil, errUnavailable
	}
	cmd := exec.Command(executable, InternalArgument)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.ExtraFiles = []*os.File{childIn, childOut}
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		childIn.Close()
		childOut.Close()
		parentIn.Close()
		parentOut.Close()
		return nil, errUnavailable
	}
	childIn.Close()
	childOut.Close()
	stopStartup := context.AfterFunc(ctx, func() { parentIn.Close(); parentOut.Close(); cmd.Process.Kill() })
	defer stopStartup()
	h := &Child{cmd: cmd, input: parentIn, output: parentOut, done: make(chan error, 1), socket: filepath.Join(options.RuntimeDir, "rewrite-"+options.Generation+".sock"), generation: options.Generation}
	go func() { h.done <- cmd.Wait(); close(h.done) }()
	parentIn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	parentOut.SetReadDeadline(time.Now().Add(5 * time.Second))
	if writeFrame(parentIn, startup{RuntimeDir: options.RuntimeDir, Generation: options.Generation, OwnerUID: os.Geteuid(), Version: 2, CADir: options.CADir, Document: options.Document}) != nil {
		h.Close()
		return nil, errUnavailable
	}
	var reply childReply
	if readFrame(parentOut, &reply) != nil || reply.Announcement == nil || !loopback(reply.Announcement.HandoffAddr) {
		h.Close()
		return nil, errUnavailable
	}
	if ctx.Err() != nil {
		h.Close()
		return nil, errUnavailable
	}
	h.Announcement = *reply.Announcement
	parentIn.SetWriteDeadline(time.Time{})
	parentOut.SetReadDeadline(time.Time{})
	return h, nil
}
func (c *Child) Configure(config Configuration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.input.SetWriteDeadline(time.Now().Add(5 * time.Second))
	c.output.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer c.input.SetWriteDeadline(time.Time{})
	defer c.output.SetReadDeadline(time.Time{})
	if writeFrame(c.input, childCommand{Configure: &config}) != nil {
		return errUnavailable
	}
	var reply childReply
	if readFrame(c.output, &reply) != nil || !reply.Ready {
		return errUnavailable
	}
	c.input.Close()
	c.output.Close()
	return nil
}
func (c *Child) Done() <-chan error { return c.done }
func (c *Child) Close() error {
	c.once.Do(func() {
		c.input.Close()
		c.output.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, _ = Control(ctx, c.socket, "POST", "/shutdown", c.generation)
		cancel()
		select {
		case <-c.done:
		case <-time.After(5 * time.Second):
			c.cmd.Process.Kill()
			<-c.done
		}
	})
	return nil
}

func exactKeys(b []byte, t reflect.Type) bool {
	if t == reflect.TypeOf(json.RawMessage{}) {
		return true
	}
	if t.Kind() == reflect.Pointer {
		if string(b) == "null" {
			return true
		}
		return exactKeys(b, t.Elem())
	}
	if t.Kind() != reflect.Struct {
		return true
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(b, &fields) != nil || fields == nil {
		return false
	}
	allowed := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if tag != "" && tag != "-" {
			allowed[tag] = f.Type
		}
	}
	for k, v := range fields {
		typ, ok := allowed[k]
		if !ok || !exactKeys(v, typ) {
			return false
		}
	}
	return true
}
func (c *Child) Socket() string { return c.socket }
func (c *Child) PID() int       { return c.cmd.Process.Pid }
