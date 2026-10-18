package requestrewriteservice

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrivateControlStatusDrainAndPaths(t *testing.T) {
	f := newFixture(t, "127.0.0.1:1")
	dir, e := os.MkdirTemp("", "nc-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	socket, e := f.s.ListenControl(dir, os.Geteuid())
	if e != nil {
		t.Fatal(e)
	}
	status, e := Control(context.Background(), socket, "GET", "/status", f.config.Generation)
	if e != nil || !status.Ready || status.Generation != f.config.Generation {
		t.Fatalf("status %+v %v", status, e)
	}
	if _, e = Control(context.Background(), socket, "POST", "/drain", "00000000000000000000000000000000"); e == nil {
		t.Fatal("stale generation accepted")
	}
	status, e = Control(context.Background(), socket, "POST", "/drain", f.config.Generation)
	if e != nil || status.Ready {
		t.Fatalf("drain %+v %v", status, e)
	}
	st, e := os.Stat(socket)
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal("public control socket")
	}
	other, e := New(f.s.snapshot, f.s.ca)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	if e = other.Configure(f.config); e != nil {
		t.Fatal(e)
	}
	if _, e = other.ListenControl(dir, os.Geteuid()); e == nil {
		t.Fatal("duplicate owner accepted")
	}
	target := t.TempDir()
	link := filepath.Join(dir, "link")
	os.Symlink(target, link)
	if _, e = other.ListenControl(link, os.Geteuid()); e == nil {
		t.Fatal("symlink accepted")
	}
	os.Chmod(target, 0755)
	if _, e = other.ListenControl(target, os.Geteuid()); e == nil {
		t.Fatal("public runtime accepted")
	}
	writable := filepath.Join(dir, "writable")
	os.Mkdir(writable, 0777)
	os.Chmod(writable, 0777)
	private := filepath.Join(writable, "private")
	os.Mkdir(private, 0700)
	if _, e = other.ListenControl(private, os.Geteuid()); e == nil {
		t.Fatal("untrusted ancestor accepted")
	}
}
func TestPendingHandshakeBound(t *testing.T) {
	f := newFixture(t, "127.0.0.1:1")
	var sockets []net.Conn
	defer func() {
		for _, c := range sockets {
			c.Close()
		}
	}()
	for i := 0; i < 32; i++ {
		c, e := net.Dial("tcp4", f.s.Announcement().HandoffAddr)
		if e != nil {
			t.Fatal(e)
		}
		sockets = append(sockets, c)
	}
	deadline := time.Now().Add(time.Second)
	for len(f.s.pending) < 32 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(f.s.pending) != 32 {
		t.Fatal("pending stages not admitted")
	}
	extra, e := net.Dial("tcp4", f.s.Announcement().HandoffAddr)
	if e != nil {
		t.Fatal(e)
	}
	defer extra.Close()
	extra.SetReadDeadline(time.Now().Add(time.Second))
	b := make([]byte, 1)
	if _, e = extra.Read(b); e == nil {
		t.Fatal("excess handshake accepted")
	}
}
