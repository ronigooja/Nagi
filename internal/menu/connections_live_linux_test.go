package menu

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type liveCopyOutput struct {
	sync.Mutex
	buffer bytes.Buffer
	writes int
}

func (out *liveCopyOutput) Write(p []byte) (int, error) {
	out.Lock()
	defer out.Unlock()
	out.writes++
	return out.buffer.Write(p)
}

func (out *liveCopyOutput) snapshot() (string, int) {
	out.Lock()
	defer out.Unlock()
	return out.buffer.String(), out.writes
}

type liveCopyRunner struct{ samples atomic.Int64 }

func (*liveCopyRunner) Run(context.Context, []string) Result { return Result{} }
func (r *liveCopyRunner) Stream(ctx context.Context, _ []string, out io.Writer) Result {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		n := r.samples.Add(1)
		_, err := fmt.Fprintf(out, `{"ok":true,"data":{"connections":[{"id":"copy-fixture","metadata":{"host":"copy.example"},"upload":%d}]}}`+"\n", n*1024)
		if err != nil {
			return Result{}
		}
		select {
		case <-ctx.Done():
			return Result{}
		case <-ticker.C:
		}
	}
}

// This PTY runs in the normal test suite: continuously changing snapshots must
// not write to the screen while the user has frozen it to select/copy text.
func TestLiveCopyFreezeKeepsTerminalQuietWhileCollecting(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "1")
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer slave.Close()
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 80}); err != nil {
		t.Fatal(err)
	}
	before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	runner, output := &liveCopyRunner{}, &liveCopyOutput{}
	s := NewSession(runner, slave, output)
	s.interrupts = make(chan os.Signal, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	done := make(chan struct{})
	var result error
	go func() { result = liveConnections(ctx, s); close(done) }()
	defer func() { cancel(); <-done }()
	waitFor := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for !check() {
			if time.Now().After(deadline) {
				t.Fatal("live view did not reach expected state")
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitFor(func() bool { text, _ := output.snapshot(); return strings.Contains(text, "copy.example") })
	if _, err := master.WriteString("p\r"); err != nil {
		t.Fatal(err)
	}
	waitFor(func() bool { text, _ := output.snapshot(); return strings.Contains(text, "Frozen; select text") })
	_, writes := output.snapshot()
	samples := runner.samples.Load()
	time.Sleep(650 * time.Millisecond)
	if _, now := output.snapshot(); now != writes {
		t.Fatal("incoming samples rewrote the screen during text selection")
	}
	if runner.samples.Load() <= samples {
		t.Fatal("freezing the display also stopped collection")
	}
	if _, err := master.WriteString("p\r"); err != nil {
		t.Fatal(err)
	}
	waitFor(func() bool { _, now := output.snapshot(); return now > writes })
	if _, err := master.WriteString("q\r"); err != nil {
		t.Fatal(err)
	}
	<-done
	if result != ErrBack {
		t.Fatalf("exit = %v", result)
	}
	after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil || *after != *before {
		t.Fatalf("terminal state was not restored: %v", err)
	}
	text, _ := output.snapshot()
	if !strings.HasSuffix(text, "\x1b[?25h\x1b[?1049l") {
		t.Fatal("original terminal screen was not restored")
	}
	if strings.ContainsAny(text, "\r\n") {
		t.Fatal("live view emitted scrolling line breaks")
	}
}
