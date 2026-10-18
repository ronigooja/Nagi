// Package installtx provides durable file transactions for installation maintenance.
// A prepared journal is independent of install-state and is always recovered before
// a new transaction. Privileged helper and service changes remain caller-owned.
package installtx

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type Operation struct {
	Target  string `json:"target"`
	Source  string `json:"source,omitempty"`
	HadOld  bool   `json:"had_old"`
	OldHash string `json:"old_hash,omitempty"`
	NewHash string `json:"new_hash,omitempty"`
}
type manifest struct {
	Version    int         `json:"version"`
	Root       string      `json:"root"`
	Operations []Operation `json:"operations"`
}

var syncHook = func(string) error { return nil } // Tests inject storage errors.
var checkpoint = func(string) {}                 // Tests replace this with abrupt-process-exit injection.

// cleanPath refuses symlinks in every existing component, including the leaf.
func cleanPath(p string) error {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" {
		return fmt.Errorf("unsafe transaction path %q", p)
	}
	for q := p; q != "/"; q = filepath.Dir(q) {
		s, e := os.Lstat(q)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if s.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink transaction path %q", q)
		}
	}
	return nil
}
func syncFile(f *os.File) error {
	if e := syncHook(f.Name()); e != nil {
		return e
	}
	return fullSync(f)
}
func syncDir(p string) error {
	if e := syncHook(p); e != nil {
		return e
	}
	f, e := os.Open(p)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func durableMkdir(p string, mode os.FileMode) error {
	if s, e := os.Lstat(p); e == nil {
		if !s.IsDir() {
			return fmt.Errorf("not a directory: %s", p)
		}
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	if e := durableMkdir(filepath.Dir(p), mode); e != nil {
		return e
	}
	if e := os.Mkdir(p, mode); e != nil {
		return e
	}
	return syncDir(filepath.Dir(p))
}
func privateRoot(root string) error {
	if e := cleanPath(root); e != nil {
		return e
	}
	if e := durableMkdir(root, 0700); e != nil {
		return e
	}
	s, e := os.Stat(root)
	if e != nil {
		return e
	}
	st := s.Sys().(*syscall.Stat_t)
	if s.Mode().Perm()&0077 != 0 || int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("transaction journal must be private and owned by current user: %s", root)
	}
	return nil
}
func copyTree(src, dst string) error {
	s, e := os.Lstat(src)
	if e != nil {
		return e
	}
	if s.Mode()&os.ModeSymlink != 0 || (!s.IsDir() && !s.Mode().IsRegular()) {
		return fmt.Errorf("unsupported transaction source %s", src)
	}
	if s.IsDir() {
		if e = os.Mkdir(dst, 0700); e != nil {
			return e
		}
		entries, e := os.ReadDir(src)
		if e != nil {
			return e
		}
		for _, v := range entries {
			if e = copyTree(filepath.Join(src, v.Name()), filepath.Join(dst, v.Name())); e != nil {
				return e
			}
		}
		if e = metadata(dst, s); e != nil {
			return e
		}
		return syncDir(dst)
	}
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, s.Mode().Perm())
	if e != nil {
		return e
	}
	_, e = io.Copy(out, in)
	if e == nil {
		e = metadata(dst, s)
	}
	if e == nil {
		e = syncFile(out)
	}
	closeErr := out.Close()
	if e == nil {
		e = closeErr
	}
	return e
}
func metadata(path string, s os.FileInfo) error {
	st := s.Sys().(*syscall.Stat_t)
	if os.Geteuid() == 0 {
		if e := os.Chown(path, int(st.Uid), int(st.Gid)); e != nil {
			return e
		}
	}
	return os.Chmod(path, s.Mode().Perm())
}
func writeJSON(path string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	if e == nil {
		e = syncFile(f)
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	return e
}
func publishCommit(root string) error {
	f, e := os.CreateTemp(root, ".commit-")
	if e != nil {
		return e
	}
	p := f.Name()
	defer os.Remove(p)
	_, e = f.WriteString("true")
	if e == nil {
		e = syncFile(f)
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(p, filepath.Join(root, "committed")); e != nil {
		return e
	}
	if e = syncDir(root); e != nil {
		removeErr := os.Remove(filepath.Join(root, "committed"))
		syncErr := syncDir(root)
		return errors.Join(e, removeErr, syncErr)
	}
	return nil
}
func load(root string) (manifest, error) {
	var m manifest
	if e := privateRoot(root); e != nil {
		return m, e
	}
	p := filepath.Join(root, "manifest.json")
	if st, e := os.Lstat(p); e == nil {
		if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || int(st.Sys().(*syscall.Stat_t).Uid) != os.Geteuid() {
			return m, errors.New("unsafe transaction manifest")
		}
	} else if !os.IsNotExist(e) {
		return m, e
	}
	if e := cleanPath(p); e != nil {
		return m, e
	}
	f, e := os.Open(p)
	if e != nil {
		return m, e
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if e = d.Decode(&m); e != nil {
		return m, e
	}
	if d.Decode(new(any)) != io.EOF {
		return m, errors.New("trailing manifest data")
	}
	if m.Version != 1 || m.Root != root || len(m.Operations) == 0 {
		return m, errors.New("invalid transaction manifest")
	}
	for i, o := range m.Operations {
		for j := 0; j < i; j++ {
			p := m.Operations[j].Target
			if p == o.Target || inside(p, o.Target) || inside(o.Target, p) {
				return m, errors.New("overlapping manifest targets")
			}
		}
		if e = validateTarget(root, o.Target); e != nil {
			return m, e
		}
		if o.Source != "" && o.Source != fmt.Sprintf("new-%d", i) {
			return m, errors.New("invalid staged source")
		}
	}
	return m, nil
}
func validateTarget(root, target string) error {
	if e := cleanPath(target); e != nil {
		return e
	}
	if target == root || inside(root, target) || inside(target, root) || target == root+".lock" {
		return errors.New("target overlaps transaction journal")
	}
	return nil
}
func inside(parent, child string) bool {
	r, e := filepath.Rel(parent, child)
	return e == nil && r != "." && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// CommittedError means file changes committed durably, but cleanup needs recovery.
type CommittedError struct{ Err error }

func (e *CommittedError) Error() string {
	return "installation files committed; journal cleanup pending: " + e.Err.Error()
}
func (e *CommittedError) Unwrap() error { return e.Err }
func syncAncestors(p string) error {
	for {
		if e := syncDir(p); e != nil {
			return e
		}
		parent := filepath.Dir(p)
		if parent == p {
			return nil
		}
		p = parent
	}
}

// Prepare preserves both new and original trees before publishing the manifest.
// Callers must hold WithLock throughout Prepare, Activate, and Commit.
func Prepare(root string, ops []Operation) error {
	if len(ops) == 0 {
		return errors.New("empty transaction")
	}
	if e := Recover(root); e != nil {
		return e
	}
	if e := privateRoot(root); e != nil {
		return e
	}
	seen := []string{}
	m := manifest{Version: 1, Root: root}
	for i, o := range ops {
		if e := validateTarget(root, o.Target); e != nil {
			return e
		}
		for _, p := range seen {
			if p == o.Target || inside(p, o.Target) || inside(o.Target, p) {
				return errors.New("overlapping transaction targets")
			}
		}
		seen = append(seen, o.Target)
		if e := cleanPath(filepath.Dir(o.Target)); e != nil {
			return e
		}
		if _, e := os.Stat(filepath.Dir(o.Target)); e != nil {
			return e
		}
		if e := syncAncestors(filepath.Dir(o.Target)); e != nil {
			return e
		}
		n := Operation{Target: o.Target}
		if _, e := os.Lstat(o.Target); e == nil {
			n.HadOld = true
			if e = copyTree(o.Target, filepath.Join(root, fmt.Sprintf("old-%d", i))); e != nil {
				return e
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		if o.Source != "" && o.Source != "-" {
			parent, e := filepath.EvalSymlinks(filepath.Dir(o.Source))
			if e != nil {
				return e
			}
			source := filepath.Join(parent, filepath.Base(o.Source))
			if e = cleanPath(source); e != nil {
				return e
			}
			n.Source = fmt.Sprintf("new-%d", i)
			if e := copyTree(source, filepath.Join(root, n.Source)); e != nil {
				return e
			}
		}
		if n.HadOld {
			var e error
			n.OldHash, e = treeHash(filepath.Join(root, fmt.Sprintf("old-%d", i)))
			if e != nil {
				return e
			}
		}
		if n.Source != "" {
			var e error
			n.NewHash, e = treeHash(filepath.Join(root, n.Source))
			if e != nil {
				return e
			}
		}
		m.Operations = append(m.Operations, n)
	}
	if e := syncDir(root); e != nil {
		return e
	}
	if e := writeJSON(filepath.Join(root, "manifest.tmp"), m); e != nil {
		return e
	}
	if e := os.Rename(filepath.Join(root, "manifest.tmp"), filepath.Join(root, "manifest.json")); e != nil {
		return e
	}
	if e := syncDir(root); e != nil {
		return e
	}
	checkpoint("prepared")
	return nil
}

// replace makes an independent neighbor copy; recovery never consumes its backups.
func replace(src, target string) error {
	if e := cleanPath(target); e != nil {
		return e
	}
	if src == "" {
		if e := os.RemoveAll(target); e != nil {
			return e
		}
		return syncDir(filepath.Dir(target))
	}
	tmp, e := os.MkdirTemp(filepath.Dir(target), ".nagi-transaction-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	p := filepath.Join(tmp, "content")
	if e = copyTree(src, p); e != nil {
		return e
	}
	if e = syncDir(tmp); e != nil {
		return e
	}
	if s, se := os.Lstat(target); se == nil && s.IsDir() {
		if e = os.RemoveAll(target); e != nil {
			return e
		}
	} else if se != nil && !os.IsNotExist(se) {
		return se
	}
	if e = os.Rename(p, target); e != nil {
		return e
	}
	if e = syncDir(tmp); e != nil {
		return e
	}
	return syncDir(filepath.Dir(target))
}
func treeHash(path string) (string, error) {
	h := sha256.New()
	err := filepath.Walk(path, func(p string, s os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if !s.IsDir() && !s.Mode().IsRegular() {
			return errors.New("non-regular transaction content")
		}
		rel, e := filepath.Rel(path, p)
		if e != nil {
			return e
		}
		st := s.Sys().(*syscall.Stat_t)
		fmt.Fprintf(h, "%d:%s:%o:%d:%d\n", len(rel), rel, s.Mode(), st.Uid, st.Gid)
		if s.IsDir() {
			return nil
		}
		fmt.Fprintf(h, "%d\n", s.Size())
		f, e := os.Open(p)
		if e != nil {
			return e
		}
		_, e = io.Copy(h, f)
		ce := f.Close()
		if e == nil {
			e = ce
		}
		return e
	})
	return hex.EncodeToString(h.Sum(nil)), err
}
func verify(root string, m manifest) error {
	for i, o := range m.Operations {
		for _, v := range []struct{ path, hash string }{{fmt.Sprintf("old-%d", i), o.OldHash}, {o.Source, o.NewHash}} {
			if v.hash == "" {
				if v.path == o.Source && o.Source == "" {
					continue
				}
				if v.path == fmt.Sprintf("old-%d", i) && !o.HadOld {
					continue
				}
				return errors.New("missing transaction digest")
			}
			if e := cleanPath(filepath.Join(root, v.path)); e != nil {
				return e
			}
			got, e := treeHash(filepath.Join(root, v.path))
			if e != nil {
				return e
			}
			if got != v.hash {
				return fmt.Errorf("transaction content corrupt; retained at %s", root)
			}
		}
	}
	return nil
}
func Activate(root string) error {
	m, e := load(root)
	if e != nil {
		return e
	}
	if e = verify(root, m); e != nil {
		return e
	}
	if _, e = os.Lstat(filepath.Join(root, "committed")); e == nil {
		return errors.New("transaction already committed")
	}
	for i, o := range m.Operations {
		src := ""
		if o.Source != "" {
			src = filepath.Join(root, o.Source)
		}
		if e = replace(src, o.Target); e != nil {
			return e
		}
		checkpoint(fmt.Sprintf("applied-%d", i))
	}
	return nil
}
func cleanup(root string) error {
	garbage, e := os.MkdirTemp(filepath.Dir(root), filepath.Base(root)+".garbage-")
	if e != nil {
		return e
	}
	if e = syscall.Rename(root, garbage); e != nil {
		os.Remove(garbage)
		return e
	}
	if e = syncDir(filepath.Dir(root)); e != nil {
		return e
	}
	checkpoint("cleanup-detached")
	if e = os.RemoveAll(garbage); e != nil {
		return e
	}
	return syncDir(filepath.Dir(root))
}
func Commit(root string) error {
	m, e := load(root)
	if e != nil {
		return e
	}
	if e = verify(root, m); e != nil {
		return e
	}
	for _, o := range m.Operations {
		if o.Source == "" {
			if _, e = os.Lstat(o.Target); !os.IsNotExist(e) {
				return errors.New("removed transaction target reappeared")
			}
		} else {
			got, e := treeHash(o.Target)
			if e != nil {
				return e
			}
			if got != o.NewHash {
				return errors.New("activated transaction target differs from prepared content")
			}
		}
	}
	if e := publishCommit(root); e != nil {
		return e
	}
	checkpoint("committed")
	if e := cleanup(root); e != nil {
		return &CommittedError{Err: e}
	}
	return nil
}

// Recover is idempotent even when a previous rollback was interrupted.
func Recover(root string) error {
	if e := cleanPath(root); e != nil {
		return e
	}
	if _, e := os.Lstat(root); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	if e := privateRoot(root); e != nil {
		return e
	}
	m, e := load(root)
	if os.IsNotExist(e) {
		if _, me := os.Lstat(filepath.Join(root, "committed")); me == nil {
			return errors.New("commit marker without manifest; journal retained")
		} else if !os.IsNotExist(me) {
			return me
		}
		return cleanup(root)
	}
	if e != nil {
		return e
	}
	marker := filepath.Join(root, "committed")
	if st, e := os.Lstat(marker); e == nil {
		if !st.Mode().IsRegular() || st.Size() != 4 {
			return errors.New("invalid commit marker")
		}
		b, e := os.ReadFile(marker)
		if e != nil {
			return e
		}
		if string(b) != "true" {
			return errors.New("invalid commit marker")
		}
		return cleanup(root)
	} else if !os.IsNotExist(e) {
		return e
	}
	if e = verify(root, m); e != nil {
		return e
	}
	for i := len(m.Operations) - 1; i >= 0; i-- {
		o := m.Operations[i]
		src := ""
		if o.HadOld {
			src = filepath.Join(root, fmt.Sprintf("old-%d", i))
			if e = cleanPath(src); e != nil {
				return e
			}
		}
		if e = replace(src, o.Target); e != nil {
			return fmt.Errorf("rollback incomplete; journal retained at %s: %w", root, e)
		}
		checkpoint(fmt.Sprintf("rollback-%d", i))
	}
	// Rollback completion must itself become durable before deleting backups.
	if e = publishCommit(root); e != nil {
		return e
	}
	return cleanup(root)
}

// WithLock also accepts a lock inherited from the hidden helper's lock command.
func WithLock(root string, fn func() error) error {
	if e := cleanPath(root); e != nil {
		return e
	}
	if e := durableMkdir(filepath.Dir(root), 0755); e != nil {
		return e
	}
	p := root + ".lock"
	if e := cleanPath(p); e != nil {
		return e
	}
	if os.Getenv("NAGI_INSTALLTX_LOCK_ROOT") == root {
		fd, e := strconv.Atoi(os.Getenv("NAGI_INSTALLTX_LOCK_FD"))
		if e == nil && fd >= 3 {
			dup, e := syscall.Dup(fd)
			if e != nil {
				return e
			}
			f := os.NewFile(uintptr(dup), "inherited installation lock")
			defer f.Close()
			a, e1 := f.Stat()
			b, e2 := os.Stat(p)
			if e1 == nil && e2 == nil && os.SameFile(a, b) {
				return fn()
			}
		}
		return errors.New("invalid inherited installation lock")
	}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return fmt.Errorf("installation maintenance already running: %w", e)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

// Run implements the private installer protocol before runtime configuration loads.
func Run(args []string, stdout, stderr io.Writer) int {
	err := run(args, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "nagi installation transaction:", err)
		var committed *CommittedError
		if errors.As(err, &committed) {
			return 4
		}
		return 1
	}
	return 0
}
func run(args []string, stdout, stderr io.Writer) error {
	locked := len(args) > 0 && args[0] == "--locked"
	if locked {
		args = args[1:]
	}
	if len(args) < 2 {
		return errors.New("expected verb and absolute journal root")
	}
	verb, root := args[0], args[1]
	if verb == "lock" {
		a := args[2:]
		if len(a) > 0 && a[0] == "--" {
			a = a[1:]
		}
		if len(a) == 0 {
			return errors.New("lock requires command")
		}
		if e := cleanPath(root); e != nil {
			return e
		}
		if e := durableMkdir(filepath.Dir(root), 0755); e != nil {
			return e
		}
		if e := cleanPath(root + ".lock"); e != nil {
			return e
		}
		f, e := os.OpenFile(root+".lock", os.O_CREATE|os.O_RDWR, 0600)
		if e != nil {
			return e
		}
		defer f.Close()
		if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
			return fmt.Errorf("installation maintenance already running: %w", e)
		}
		defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		c := exec.Command(a[0], a[1:]...)
		c.Stdin = os.Stdin
		c.Stdout = stdout
		c.Stderr = stderr
		c.ExtraFiles = []*os.File{f}
		env := []string{}
		for _, v := range os.Environ() {
			if !strings.HasPrefix(v, "NAGI_INSTALLTX_LOCK_") {
				env = append(env, v)
			}
		}
		if os.Geteuid() == 0 && os.Getenv("SUDO_UID") != "" {
			uid, e := strconv.Atoi(os.Getenv("SUDO_UID"))
			if e != nil || uid <= 0 {
				return errors.New("invalid invoking UID")
			}
			gid, e := strconv.Atoi(os.Getenv("SUDO_GID"))
			if e != nil || gid < 0 {
				return errors.New("invalid invoking GID")
			}
			u, e := user.LookupId(strconv.Itoa(uid))
			if e != nil {
				return e
			}
			c.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}}
			filtered := []string{}
			for _, v := range env {
				if !strings.HasPrefix(v, "SUDO_") && !strings.HasPrefix(v, "HOME=") && !strings.HasPrefix(v, "USER=") && !strings.HasPrefix(v, "LOGNAME=") {
					filtered = append(filtered, v)
				}
			}
			env = append(filtered, "HOME="+u.HomeDir, "USER="+u.Username, "LOGNAME="+u.Username)
		}
		c.Env = append(env, "NAGI_INSTALLTX_LOCK_ROOT="+root, "NAGI_INSTALLTX_LOCK_FD=3")
		return c.Run()
	}
	action := func() error {
		switch verb {
		case "recover":
			if len(args) != 2 {
				return errors.New("recover takes only root")
			}
			return Recover(root)
		case "prepare":
			if len(args) < 4 || (len(args)-2)%2 != 0 {
				return errors.New("prepare requires target/source pairs")
			}
			var ops []Operation
			for i := 2; i < len(args); i += 2 {
				ops = append(ops, Operation{Target: args[i], Source: args[i+1]})
			}
			return Prepare(root, ops)
		case "activate":
			return Activate(root)
		case "commit":
			return Commit(root)
		default:
			return errors.New("unknown transaction verb")
		}
	}
	if locked {
		if e := cleanPath(root + ".lock"); e != nil {
			return e
		}
		f, e := os.OpenFile(root+".lock", os.O_RDWR, 0600)
		if e != nil {
			return e
		}
		defer f.Close()
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			return errors.New("installation broker lock is not held")
		}
		if !errors.Is(e, syscall.EWOULDBLOCK) && !errors.Is(e, syscall.EAGAIN) {
			return e
		}
		return action()
	}
	return WithLock(root, action)
}
