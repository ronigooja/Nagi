package installation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/i18n"
)

type releaseTransport func(*http.Request) (*http.Response, error)

func (f releaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type interruptedReleaseBody struct{}

func (interruptedReleaseBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (interruptedReleaseBody) Close() error             { return nil }

func TestReleaseDownloadFallback(t *testing.T) {
	const base = "https://github.com/ronigooja/Nagi/releases"
	for _, tc := range []struct {
		name, failure, tag string
		wantProxy, wantErr bool
	}{
		{name: "direct succeeds"},
		{name: "checksum network failure", failure: "network", wantProxy: true},
		{name: "checksum HTTP failure", failure: "http", tag: "v1.0.0", wantProxy: true},
		{name: "archive HTTP failure", failure: "archive-http", wantProxy: true},
		{name: "archive interrupted", failure: "archive-body", wantProxy: true},
		{name: "checksum interrupted", failure: "checksum-body", wantProxy: true},
		{name: "both sources fail", failure: "both", wantProxy: true, wantErr: true},
		{name: "proxy checksum mismatch", failure: "proxy-corrupt", wantProxy: true, wantErr: true},
		{name: "direct checksum mismatch", failure: "corrupt", wantErr: true},
		{name: "malformed checksums", failure: "malformed", wantErr: true},
		{name: "canceled", failure: "cancel", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archive := archiveBytes(t, map[string]string{
				"nagi": "#!/bin/sh\nexit 0\n", "mihomo": "#!/bin/sh\nexit 0\n", "installer.sh": "#!/bin/sh\nexit 0\n",
			}, "")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var requests []string
			client := &http.Client{Transport: releaseTransport(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.URL.String())
				proxy := r.URL.Host == "gh-proxy.com"
				sums := strings.HasSuffix(r.URL.Path, "/SHA256SUMS")
				status := http.StatusOK
				body := io.NopCloser(bytes.NewReader(archive))
				if sums {
					digest := fmt.Sprintf("%x", sha256.Sum256(archive))
					if tc.failure == "corrupt" || (proxy && tc.failure == "proxy-corrupt") {
						digest = strings.Repeat("0", 64)
					}
					body = io.NopCloser(strings.NewReader(digest + "  nagi-linux-amd64.tar.gz\n"))
				}
				if tc.failure == "both" {
					status = http.StatusBadGateway
				} else if !proxy {
					switch tc.failure {
					case "network", "proxy-corrupt":
						return nil, errors.New("connection refused")
					case "http":
						status = http.StatusForbidden
					case "archive-http":
						if !sums {
							status = http.StatusBadGateway
						}
					case "archive-body":
						if !sums {
							body = interruptedReleaseBody{}
						}
					case "checksum-body":
						body = interruptedReleaseBody{}
					case "malformed":
						body = io.NopCloser(strings.NewReader("invalid\n"))
					case "cancel":
						cancel()
						return nil, context.Canceled
					}
				}
				return &http.Response{StatusCode: status, Body: body, ContentLength: int64(len(archive)), Header: make(http.Header)}, nil
			})}
			var stages []string
			artifact, cleanup, err := fetchWithProgress(ctx, base, "linux-amd64", tc.tag, client, func(message *i18n.Message) {
				stages = append(stages, message.Descriptor().ID)
			})
			if cleanup != nil {
				defer cleanup()
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error %t", err, tc.wantErr)
			}
			if err == nil {
				if _, err := os.Stat(artifact + "/nagi"); err != nil {
					t.Fatal(err)
				}
			}
			if err == nil {
				lastTransfer, verification := -1, -1
				for index, stage := range stages {
					if stage == "system.installation.progress.transfer.total" {
						lastTransfer = index
					}
					if stage == "system.installation.progress.checksum" {
						verification = index
					}
				}
				if lastTransfer < 0 || verification <= lastTransfer {
					t.Fatalf("missing transfer reports before verification: %v", stages)
				}
			}
			var proxyRequests []string
			counts := make(map[string]int)
			for _, request := range requests {
				counts[request]++
				if counts[request] != 1 {
					t.Fatalf("retried the same source request: %v", requests)
				}
				if strings.HasPrefix(request, "https://gh-proxy.com/") {
					proxyRequests = append(proxyRequests, request)
				}
			}
			if (len(proxyRequests) > 0) != tc.wantProxy {
				t.Fatalf("unexpected requests: %v", requests)
			}
			fallbackCount := 0
			for _, stage := range stages {
				if stage == "system.installation.progress.fallback" {
					fallbackCount++
				}
			}
			if (fallbackCount == 1) != tc.wantProxy {
				t.Fatalf("unexpected fallback stages: %v", stages)
			}
			if tc.wantProxy {
				suffix := "/latest/download/SHA256SUMS"
				if tc.tag != "" {
					suffix = "/download/" + tc.tag + "/SHA256SUMS"
				}
				if proxyRequests[0] != "https://gh-proxy.com/"+base+suffix {
					t.Fatalf("unexpected proxy URL: %s", proxyRequests[0])
				}
			}
		})
	}
}

func TestReleaseHeaderTimeoutDoesNotLimitBodyTransfer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for range 3 {
			time.Sleep(30 * time.Millisecond)
			fmt.Fprint(w, "part")
			w.(http.Flusher).Flush()
		}
	}))
	defer server.Close()
	client := releaseHTTPClient(&http.Client{Timeout: time.Millisecond})
	defer client.CloseIdleConnections()
	// Shrink only the header deadline; the body must be allowed to outlast it.
	client.Transport.(*http.Transport).ResponseHeaderTimeout = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "partpartpart" {
		t.Fatalf("body transfer was cut short: %q, %v", body, err)
	}
}

func TestReleaseSpeedMonitorStopsStalledAndSlowBodies(t *testing.T) {
	for _, trickle := range []bool{false, true} {
		t.Run(fmt.Sprintf("trickle=%t", trickle), func(t *testing.T) {
			reader, writer := io.Pipe()
			body := watchReleaseBody(reader, releaseMinimumSpeed, 10*time.Millisecond)
			defer body.Close()
			defer writer.Close()
			if trickle {
				go func() {
					for {
						time.Sleep(5 * time.Millisecond)
						if _, err := writer.Write([]byte("a")); err != nil {
							return
						}
					}
				}()
			}
			result := make(chan error, 1)
			go func() { _, err := io.Copy(io.Discard, body); result <- err }()
			select {
			case err := <-result:
				if err == nil || !strings.Contains(err.Error(), "below 256 KiB/s") {
					t.Fatalf("unexpected read error: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("slow body did not stop")
			}
		})
	}
}

func TestReleaseSpeedMonitorAllowsCompletedSmallFile(t *testing.T) {
	body := watchReleaseBody(io.NopCloser(strings.NewReader("checksum")), releaseMinimumSpeed, time.Millisecond)
	data, err := io.ReadAll(body)
	if err != nil || string(data) != "checksum" {
		t.Fatalf("small response: %q, %v", data, err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-body.stopped:
	default:
		t.Fatal("monitor was not joined")
	}
}

func TestReleaseCustomSourceDoesNotUsePublicProxy(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: releaseTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Host != "releases.example.invalid" {
			t.Fatalf("unexpected URL: %s", r.URL)
		}
		return nil, errors.New("offline")
	})}
	_, _, err := fetchWithClient(context.Background(), "https://releases.example.invalid", "linux-amd64", "", client)
	if err == nil || requests != 1 {
		t.Fatalf("requests = %d, error = %v", requests, err)
	}
}

// A fast first window must not disable checks for the rest of the transfer.
func TestReleaseSpeedMonitorDetectsLaterSlowdown(t *testing.T) {
	reader, writer := io.Pipe()
	body := watchReleaseBody(reader, releaseMinimumSpeed, 10*time.Millisecond)
	defer body.Close()
	defer writer.Close()
	go func() {
		until := time.Now().Add(200 * time.Millisecond)
		chunk := make([]byte, 64*1024)
		for time.Now().Before(until) {
			if _, err := writer.Write(chunk); err != nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		// Leave the body open and blocked after a fast start.
	}()
	started := time.Now()
	result := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, body); result <- err }()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "below 256 KiB/s") {
			t.Fatalf("unexpected read error: %v", err)
		}
		if time.Since(started) < 200*time.Millisecond {
			t.Fatal("fast initial transfer was rejected")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("later stall did not stop")
	}
}

func TestReleaseProgressKnownAndUnknownLength(t *testing.T) {
	for _, total := range []int64{0, -1, 2 << 20} {
		var messages []*i18n.Message
		report := releaseProgress("nagi-linux-amd64.tar.gz", total, func(m *i18n.Message) { messages = append(messages, m) })
		start := time.Now()
		report(0, start)
		report(1<<20, start.Add(time.Second))
		report(1<<20, start.Add(2*time.Second))
		for _, message := range messages {
			if !i18n.ValidDescriptor(message.Descriptor()) {
				t.Fatalf("invalid descriptor: %+v", message.Descriptor())
			}
			for _, lang := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
				if got := i18n.New(lang).Render(message); !strings.Contains(got, "nagi-linux-amd64.tar.gz") {
					t.Fatalf("missing asset: %s", got)
				}
			}
		}
		if got := messages[1].Error(); !strings.Contains(got, "1.00 MiB") || !strings.Contains(got, "1024.0 KiB/s") {
			t.Fatalf("transfer report: %s", got)
		}
		if got := messages[2].Error(); !strings.Contains(got, "0.0 KiB/s") {
			t.Fatalf("stall report: %s", got)
		}
		if got := messages[1].Error(); strings.Contains(got, "50.0%") != (total > 0) {
			t.Fatalf("percentage: %s", got)
		}
	}
}

func TestReleaseProgressReportsWhileReadBlockedAndStopsOnClose(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	var reports []int64
	body := watchReleaseBodyWithProgress(reader, releaseMinimumSpeed, 5*time.Millisecond, func(n int64, _ time.Time) {
		reports = append(reports, n)
	})
	_, err := io.ReadAll(body)
	if err == nil {
		t.Fatal("expected stalled transfer failure")
	}
	body.Close()
	count := len(reports)
	if count < 3 {
		t.Fatalf("missing live stalled reports: %v", reports)
	}
	body.Close()
	if len(reports) != count {
		t.Fatal("close reported twice")
	}
	for _, n := range reports {
		if n != 0 {
			t.Fatalf("unexpected bytes: %d", n)
		}
	}
}
