package command

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
)

type cancelWriter struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancelWriter) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	w.cancel()
	return n, err
}

func trafficServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mihomo.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); _ = listener.Close() })
	return path
}

func TestTrafficWatchStreamsJSONAndCancels(t *testing.T) {
	path := trafficServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/traffic" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte("{\"up\":12,\"down\":34,\"upTotal\":56,\"downTotal\":78}\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stdout := &cancelWriter{cancel: cancel}
	var stderr bytes.Buffer
	if code := watchTraffic(ctx, control.New(path), stdout, &stderr, true); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			UploadBPS     int64  `json:"upload_bps"`
			DownloadBPS   int64  `json:"download_bps"`
			UploadTotal   int64  `json:"upload_total"`
			DownloadTotal int64  `json:"download_total"`
			SampledAt     string `json:"sampled_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || !envelope.OK || envelope.Data.UploadBPS != 12 || envelope.Data.DownloadBPS != 34 || envelope.Data.UploadTotal != 56 || envelope.Data.DownloadTotal != 78 {
		t.Fatalf("response=%s err=%v", stdout.String(), err)
	}
	if _, err := time.Parse(time.RFC3339Nano, envelope.Data.SampledAt); err != nil {
		t.Fatalf("sampled_at=%q: %v", envelope.Data.SampledAt, err)
	}
}

func TestTrafficWatchRejectsMalformedSample(t *testing.T) {
	path := trafficServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{\"up\":1,\"down\":2,\"upTotal\":3}\n"))
	})
	var stdout, stderr bytes.Buffer
	if code := watchTraffic(context.Background(), control.New(path), &stdout, &stderr, true); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"code":"mihomo_api_error"`) {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestTrafficWatchTextAndAPIError(t *testing.T) {
	path := trafficServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{\"up\":1,\"down\":2,\"upTotal\":3,\"downTotal\":4}\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stdout := &cancelWriter{cancel: cancel}
	var stderr bytes.Buffer
	if code := watchTraffic(ctx, control.New(path), stdout, &stderr, false); code != 0 || !strings.Contains(stdout.String(), "Upload: 1 B/s (total 3 B)  Download: 2 B/s (total 4 B)") {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	// The running CLI reports a failed control API request on stderr.
	missing := filepath.Join(t.TempDir(), "missing.sock")
	stdout.Buffer.Reset()
	stderr.Reset()
	if code := watchTraffic(context.Background(), control.New(missing), &stdout.Buffer, &stderr, true); code != 1 || !strings.Contains(stderr.String(), `"code":"mihomo_api_error"`) || stdout.Len() != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestTrafficWatchHelpAndInvalidArgumentsWithoutRuntime(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "config-file")
	if err := os.WriteFile(bad, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", bad)
	for _, args := range [][]string{{"traffic", "watch", "--help"}, {"--json", "traffic", "watch", "extra"}} {
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr, "test", "commit")
		if len(args) == 3 && code != 0 || len(args) == 4 && code != 2 {
			t.Fatalf("args=%v exit=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
		}
		if !strings.Contains(stdout.String()+stderr.String(), "traffic watch") {
			t.Fatalf("args=%v output=%s%s", args, stdout.String(), stderr.String())
		}
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script, ok := completion(shell)
		if !ok || !strings.Contains(script, "traffic") || !strings.Contains(script, "watch") {
			t.Fatalf("%s completion missing traffic watch", shell)
		}
	}
}
