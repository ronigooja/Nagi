package menu

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rivo/uniseg"
	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/proxy"
	"github.com/ronigooja/Nagi/internal/terminal"
)

func TestMergeLiveSampleTracksDisappearReappearAndExpiry(t *testing.T) {
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	active := map[string]proxy.Connection{
		"gone": {ID: "gone", Upload: 10},
		"stay": {ID: "stay", Upload: 20},
	}
	ended := map[string]liveEnded{}
	mergeLiveSample(active, ended, map[string]proxy.Connection{"stay": {ID: "stay", Upload: 25}}, now)
	if _, ok := active["gone"]; ok || ended["gone"].connection.Upload != 10 || !ended["gone"].when.Equal(now) {
		t.Fatalf("disappearance not retained with last observed counters: active=%v ended=%v", active, ended)
	}

	later := now.Add(time.Minute)
	mergeLiveSample(active, ended, map[string]proxy.Connection{"gone": {ID: "gone", Upload: 30}, "stay": {ID: "stay"}}, later)
	if _, ok := ended["gone"]; ok || active["gone"].Upload != 30 {
		t.Fatalf("reappeared connection was not restored to active: active=%v ended=%v", active, ended)
	}

	mergeLiveSample(active, ended, map[string]proxy.Connection{"stay": {ID: "stay"}}, later)
	mergeLiveSample(active, ended, map[string]proxy.Connection{"stay": {ID: "stay"}}, later.Add(5*time.Minute))
	if _, ok := ended["gone"]; ok {
		t.Fatal("recently ended entry survived its five-minute retention")
	}
}

func TestLiveLineWriterDecodesWholeNDJSONSamples(t *testing.T) {
	out := make(chan liveSample, 2)
	writer := &liveLineWriter{ctx: context.Background(), out: out}
	first := `{"ok":true,"data":{"connections":[{"id":"one","upload":4,"chains":["Proxy"]}]}}` + "\n"
	if n, err := writer.Write([]byte(first[:15])); err != nil || n != 15 {
		t.Fatalf("first partial write: n=%d err=%v", n, err)
	}
	if n, err := writer.Write([]byte(first[15:])); err != nil || n != len(first)-15 {
		t.Fatalf("second partial write: n=%d err=%v", n, err)
	}
	sample := <-out
	if got := sample.connections["one"]; got.ID != "one" || got.Upload != 4 || strings.Join(got.Chains, ",") != "Proxy" {
		t.Fatalf("decoded sample = %#v", got)
	}
}

func TestHumanBytesUsesReadableBinaryUnits(t *testing.T) {
	for _, tc := range []struct {
		n    int64
		want string
	}{{0, "0B"}, {1024, "1.0KiB"}, {1048576, "1.0MiB"}} {
		if got := humanBytes(tc.n); got != tc.want {
			t.Errorf("humanBytes(%d)=%q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestLiveLineWriterCancellationUnblocksSlowConsumer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	writer := &liveLineWriter{ctx: ctx, out: make(chan liveSample), beforeSend: started}
	finished := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte(`{"ok":true,"data":{"connections":[]}}` + "\n"))
		finished <- err
	}()
	<-started // Parsing completed and the next operation is the blocked send.
	cancel()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("expected canceled writer")
		}
	case <-time.After(time.Second):
		t.Fatal("writer remained blocked after cancellation")
	}
}

type livePTYRunner struct {
	samples int
	exited  chan struct{}
}

func (r *livePTYRunner) Run(context.Context, []string) Result { return Result{} }
func (r *livePTYRunner) Stream(ctx context.Context, _ []string, out io.Writer) Result {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		upload := 1024
		if os.Getenv("NAGI_PTY_COPY_CHECK") == "1" {
			upload += r.samples * 1024
		}
		_, _ = fmt.Fprintf(out, `{"ok":true,"data":{"sampled_at":"2026-10-22T17:00:00Z","connections":[{"id":"fixture-id","metadata":{"host":"fixture.example","destinationPort":"443","process":"nagi-test","network":"tcp"},"chains":["Proxy"],"upload":%d,"download":2048}]}}`+"\n", upload)
		r.samples++
		select {
		case <-ctx.Done():
			if r.exited != nil {
				close(r.exited)
			}
			return Result{}
		case <-ticker.C:
		}
	}
}

// TestLiveConnectionsPTY is intended to run under a real PTY. It exercises raw
// input, live redraw, command processing, and terminal restoration.
func terminalState(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("stty", "-g")
	cmd.Stdin = os.Stdin
	data, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

func TestLiveConnectionsPTY(t *testing.T) {
	if !terminal.IsTerminal(os.Stdin) {
		t.Skip("requires a PTY")
	}
	var captured strings.Builder
	out := io.MultiWriter(os.Stdout, &captured)
	runner := &livePTYRunner{exited: make(chan struct{})}
	s := NewSession(runner, os.Stdin, out)
	s.interrupts = make(chan os.Signal, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	before := terminalState(t)
	err := liveConnections(ctx, s)
	after := terminalState(t)
	want := ErrBack
	if os.Getenv("NAGI_PTY_CTRL_D") == "1" {
		want = ErrQuit
	}
	if err != want {
		t.Fatalf("live view exit = %v, want %v", err, want)
	}
	if before != after {
		t.Fatalf("terminal state changed: before=%q after=%q", before, after)
	}
	select {
	case <-runner.exited:
	default:
		t.Fatal("watch child remained after page return")
	}
	if runner.samples == 0 {
		t.Fatal("watch stream delivered no samples")
	}
	if os.Getenv("NAGI_PTY_COPY_CHECK") == "1" && runner.samples < 10 {
		t.Fatalf("collection stopped during copy check: %d samples", runner.samples)
	}
	for _, want := range []string{"fixture-id", "fixture.example", "Proxy"} {
		if !strings.Contains(captured.String(), want) {
			t.Errorf("live view output missing %q", want)
		}
	}
}

func TestLiveFramesFitTerminalIncludingUnicodeAndLongCommands(t *testing.T) {
	var rows []liveRow
	for n := 0; n < 100; n++ {
		rows = append(rows, liveRow{connection: proxy.Connection{
			ID:       fmt.Sprintf("00000000-0000-0000-0000-%012d", n),
			Metadata: map[string]any{"host": "fixture.example", "process": "中文👩‍💻e\u0301", "network": "tcp"},
			Chains:   []string{"代理👩‍💻"}, Upload: 1024, Download: 4096,
		}})
	}
	for _, language := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
		for _, size := range [][2]int{{80, 24}, {120, 24}, {60, 16}, {40, 12}, {20, 8}} {
			for _, detail := range []*liveRow{nil, &rows[0]} {
				var frame strings.Builder
				liveRender(&frame, i18n.New(language), rows, "all", "", "target", true, 100, 0, time.Now(), strings.Repeat("中文👩‍💻e\u0301", 20), 999, detail, size[0], size[1])
				lines := strings.Split(strings.TrimSuffix(frame.String(), "\n"), "\n")
				if len(lines) != size[1] {
					t.Fatalf("%s %v: %d lines", language, size, len(lines))
				}
				for _, line := range lines {
					if uniseg.StringWidth(line) >= size[0] || strings.Contains(line, "\x1b") {
						t.Fatalf("%s %v: unsafe physical line %q", language, size, line)
					}
				}
				if !strings.HasPrefix(lines[len(lines)-1], "> ") {
					t.Fatal("command prompt lost")
				}
				if size[0] >= 60 && detail == nil && !strings.Contains(frame.String(), rows[99].connection.ID) {
					// A clamped final page must retain complete IDs in the table.
					t.Fatalf("last page missing full ID: %s", frame.String())
				}
			}
		}
	}
}

func TestLiveScreenUsesCursorUpdatesWithoutScrollOrColorDependency(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "xterm-256color")
	var out bytes.Buffer
	screen := &liveScreen{out: &out, width: 40, height: 8}
	if err := screen.enter(); err != nil {
		t.Fatal(err)
	}
	defer screen.leave()
	if !strings.Contains(out.String(), "\x1b[?1049h") {
		t.Fatal("main terminal buffer was not preserved")
	}
	out.Reset()
	frame := "Connections\n" + strings.Repeat("长", 80) + "\n> "
	if err := screen.draw(frame); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out.String(), "\r\n") || !strings.Contains(out.String(), "\x1b[2K") {
		t.Fatalf("frame can scroll or lacks in-place addressing: %q", out.String())
	}
	out.Reset()
	if err := screen.draw(frame); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("unchanged text was repainted: %q", out.String())
	}
	if err := screen.draw(strings.Replace(frame, "Connections", "Frozen", 1)); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "\x1b[2K") != 1 {
		t.Fatalf("unchanged rows were repainted: %q", out.String())
	}
	out.Reset()
	if err := screen.draw(strings.Replace(frame, "Connections", "Frozen", 1)); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("clipped text was needlessly repainted: %q", out.String())
	}
}

func TestLiveFitPreservesGraphemes(t *testing.T) {
	if got := liveFit("👩‍💻e\u0301中文", 4); got != "👩‍💻e\u0301 " {
		t.Fatalf("split grapheme: %q", got)
	}
}

func TestLiveCloseUsesFullActiveIDOnce(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	runner := &featureRunner{results: map[string]Result{"connections\x00close\x00" + id: {}}}
	s := NewSession(runner, strings.NewReader(""), &bytes.Buffer{})
	active := map[string]proxy.Connection{id: {ID: id}}
	ended, closing := map[string]liveEnded{}, map[string]bool{}
	filter, search, sortBy := "all", "", "target"
	frozen := false
	var rows []liveRow
	ac, ec := 0, 0
	var sampled time.Time
	page := 0
	var detail *liveRow
	invoke := func() error {
		return liveCommand("c "+id, s, context.Background(), active, ended, closing, &filter, &search, &sortBy, &frozen, &rows, &ac, &ec, &sampled, time.Now(), &page, &detail)
	}
	if err := invoke(); err != nil {
		t.Fatal(err)
	}
	if err := invoke(); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 || !reflect.DeepEqual(runner.calls[0], []string{"connections", "close", id}) {
		t.Fatalf("close calls = %#v", runner.calls)
	}
}

func TestLiveFreezeDetailsStayOnCapturedSnapshotAndExitKeys(t *testing.T) {
	const id = "connection-full-id-123456789"
	active := map[string]proxy.Connection{id: {ID: id, Upload: 12, Metadata: map[string]any{"host": "old.example"}}}
	ended, closing := map[string]liveEnded{}, map[string]bool{}
	s := NewSession(&featureRunner{}, strings.NewReader(""), &bytes.Buffer{})
	filter, search, sortBy := "all", "", "target"
	frozen := false
	var rows []liveRow
	ac, ec := 0, 0
	sampled := time.Now()
	page := 0
	var detail *liveRow
	invoke := func(command string) error {
		return liveCommand(command, s, context.Background(), active, ended, closing, &filter, &search, &sortBy, &frozen, &rows, &ac, &ec, &sampled, sampled, &page, &detail)
	}
	if err := invoke("p"); err != nil {
		t.Fatal(err)
	}
	active[id] = proxy.Connection{ID: id, Upload: 99, Metadata: map[string]any{"host": "new.example"}}
	if err := invoke("d " + id); err != nil {
		t.Fatal(err)
	}
	if detail == nil || detail.connection.Upload != 12 || detail.connection.Metadata["host"] != "old.example" {
		t.Fatalf("frozen detail changed: %#v", detail)
	}
	if err := invoke("q"); err != ErrBack {
		t.Fatalf("q in detail = %v", err)
	}
	if detail == nil {
		t.Fatal("q should exit the whole live view, not silently clear details")
	}
	if err := invoke("x"); err != nil || detail != nil {
		t.Fatalf("x should return to the table: detail=%v err=%v", detail, err)
	}
}
