package menu

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTrafficPageReadsOverlapAndKeepSummaryOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	commands := []string{"tun", "ports", "lan"}
	if runtime.GOOS == "darwin" {
		commands = append(commands, "system-proxy")
	}
	started := make(chan string, len(commands))
	releases := map[string]chan struct{}{}
	for _, command := range commands {
		releases[command] = make(chan struct{})
	}
	runner := NewCommandRunner(func(args []string, stdout, stderr io.Writer) int {
		if len(args) >= 2 && args[0] == "--lang" {
			args = args[2:]
		}
		if len(args) != 3 || args[0] != "--json" || args[2] != "status" {
			t.Errorf("unexpected read: %v", args)
			return 1
		}
		command := args[1]
		started <- command
		select {
		case <-releases[command]:
		case <-ctx.Done():
			return 1
		}
		if command == "lan" {
			io.WriteString(stderr, `{"ok":false,"error":{"code":"lan_error","message":"LAN read failed"}}`)
			return 1
		}
		io.WriteString(stdout, `{"ok":true,"data":{"enabled":true,"http":7890,"socks":7891,"mixed":7892}}`)
		return 0
	})
	var out bytes.Buffer
	s := NewSession(runner, strings.NewReader(""), &out)
	s.pending = "prior feedback"
	done := make(chan PageView, 1)
	go func() {
		view, err := TrafficPage().Load(ctx, s)
		if err != nil {
			t.Error(err)
		}
		done <- view
	}()
	seen := map[string]bool{}
	for len(seen) < len(commands) {
		select {
		case command := <-started:
			if seen[command] {
				t.Fatalf("read dispatched twice: %s", command)
			}
			seen[command] = true
		case <-ctx.Done():
			t.Fatal("traffic reads did not overlap")
		}
	}
	for i := len(commands) - 1; i >= 0; i-- {
		close(releases[commands[i]])
	}
	select {
	case view := <-done:
		want := "TUN: On\nListener Ports: HTTP 7890, SOCKS 7891, Mixed 7892\nLAN Access: unknown — LAN read failed"
		if runtime.GOOS == "darwin" {
			want += "\nSystem Proxy: On"
		}
		if view.Context != want || len(view.Actions) != len(commands) {
			t.Fatalf("view = %#v", view)
		}
	case <-ctx.Done():
		t.Fatal("page did not finish")
	}
	if out.Len() != 0 || s.pending != "prior feedback" {
		t.Fatalf("read changed session presentation: output=%q pending=%q", out.String(), s.pending)
	}
}

func TestReadBatchFallbackKeepsCustomRunnerSerial(t *testing.T) {
	commands := [][]string{{"tun", "status"}, {"ports", "status"}, {"lan", "status"}}
	runner := &fakeRunner{results: map[string]Result{"ports status": {Code: 1, Error: &CLIError{Code: "ports_error", Message: "failed"}}}}
	s := NewSession(runner, strings.NewReader(""), &bytes.Buffer{})
	results := s.fetchReads(context.Background(), commands)
	if !reflect.DeepEqual(runner.calls, commands) || len(results) != 3 || results[1].Error.Code != "ports_error" || results[2].Code != 0 {
		t.Fatalf("calls=%v results=%v", runner.calls, results)
	}
}

func TestTrafficMutationDispatchOnceAfterReads(t *testing.T) {
	var mu sync.Mutex
	active, mutations := 0, 0
	runner := NewCommandRunner(func(args []string, stdout, stderr io.Writer) int {
		if len(args) >= 2 && args[0] == "--lang" {
			args = args[2:]
		}
		mu.Lock()
		if args[2] == "disable" {
			mutations++
			if active != 0 {
				t.Errorf("mutation overlapped %d status reads", active)
			}
		} else {
			active++
		}
		mu.Unlock()
		io.WriteString(stdout, `{"ok":true,"data":{"enabled":false}}`)
		if args[2] != "disable" {
			mu.Lock()
			active--
			mu.Unlock()
		}
		return 0
	})
	selection := "1\n2\nq\n"
	if runtime.GOOS == "darwin" {
		selection = "2\n2\nq\n"
	}
	var out bytes.Buffer
	if code := RunPage(context.Background(), NewSession(runner, strings.NewReader(selection), &out), TrafficPage()); code != 0 {
		t.Fatalf("menu exit = %d", code)
	}
	if mutations != 1 || active != 0 {
		t.Fatalf("mutation dispatches=%d pending reads=%d", mutations, active)
	}
}
