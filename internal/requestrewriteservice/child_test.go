package requestrewriteservice

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == InternalArgument {
		os.Exit(RunChild())
	}
	os.Exit(m.Run())
}
func TestUnprivilegedPersistentChild(t *testing.T) {
	if os.Geteuid() == 0 {
		dir, e := os.MkdirTemp("", "nagi-child-test-")
		if e != nil {
			t.Fatal(e)
		}
		defer os.RemoveAll(dir)
		os.Chmod(dir, 0755)
		source, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		in, e := os.Open(source)
		if e != nil {
			t.Fatal(e)
		}
		defer in.Close()
		target := filepath.Join(dir, "fixture")
		out, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
		if e != nil {
			t.Fatal(e)
		}
		_, e = io.Copy(out, in)
		out.Close()
		if e != nil {
			t.Fatal(e)
		}
		cmd := exec.Command(target, "-test.run=^TestUnprivilegedPersistentChild$", "-test.timeout=25s")
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("unprivileged fixture: %v %s", e, b)
		}
		return
	}
	f := newFixture(t, "127.0.0.1:1")
	f.s.Close()
	runtimeDir, e := os.MkdirTemp("", "nr-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(runtimeDir)
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	child, e := StartChild(ctx, executable, ChildOptions{CADir: f.caDir, RuntimeDir: runtimeDir, Generation: f.config.Generation, Document: f.s.snapshot.ExportSensitive()})
	if e != nil {
		t.Fatal(e)
	}
	defer child.Close()
	der, e := base64.StdEncoding.DecodeString(child.Announcement.ChildTransportCert)
	if e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	f.childDER = der
	f.mu.Unlock()
	if e = child.Configure(f.config); e != nil {
		t.Fatal(e)
	}
	cancel()
	time.Sleep(30 * time.Millisecond)
	status, e := Control(context.Background(), child.Socket(), "GET", "/status", f.config.Generation)
	if e != nil || !status.Ready {
		t.Fatalf("startup EOF/caller cancel stopped child: %+v %v", status, e)
	}
	status, e = Control(context.Background(), child.Socket(), "POST", "/drain", f.config.Generation)
	if e != nil || status.Ready {
		t.Fatalf("drain %+v %v", status, e)
	}
	child.Close()
	select {
	case <-child.Done():
	case <-time.After(time.Second):
		t.Fatal("shutdown did not exit")
	}
}

func TestControlClientAuthenticatesPeerBeforeHTTP(t *testing.T) {
	if os.Geteuid() != 0 {
		socket := os.Getenv("NAGI_TEST_FOREIGN_SOCKET")
		if socket == "" {
			t.Skip("root fixture launches different client UID")
		}
		if _, e := Control(context.Background(), socket, "GET", "/status", ""); e == nil {
			t.Fatal("foreign server UID accepted")
		}
		return
	}
	dir, e := os.MkdirTemp("", "nagi-peer-test-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	os.Chmod(dir, 0755)
	socket := filepath.Join(dir, "control.sock")
	listener, e := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	os.Chmod(socket, 0777)
	received := make(chan int, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			received <- -1
			return
		}
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		b := make([]byte, 1)
		n, _ := conn.Read(b)
		received <- n
	}()
	source, _ := os.Executable()
	in, e := os.Open(source)
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	target := filepath.Join(dir, "fixture")
	out, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if e != nil {
		t.Fatal(e)
	}
	_, e = io.Copy(out, in)
	out.Close()
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(target, "-test.run=^TestControlClientAuthenticatesPeerBeforeHTTP$", "-test.timeout=5s")
	cmd.Env = append(os.Environ(), "NAGI_TEST_FOREIGN_SOCKET="+socket)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("foreign peer fixture %v %s", e, b)
	}
	select {
	case n := <-received:
		if n != 0 {
			t.Fatalf("foreign peer received %d HTTP bytes", n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("foreign peer gate stalled")
	}
}
