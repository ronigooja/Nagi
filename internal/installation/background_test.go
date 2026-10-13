package installation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestAppPreparationDownloadsThroughRunningProxyBeforeQuit(t *testing.T) {
	for _, failure := range []string{"checksum", "preflight"} {
		t.Run(failure, func(t *testing.T) {
			m, prefix := fixture(t)
			m.OS, m.Arch = "darwin", "arm64"
			app := filepath.Join(filepath.Dir(prefix), "Applications", "Nagi.app")
			os.MkdirAll(app, 0755)
			os.WriteFile(filepath.Join(prefix, "share/nagi/install-state"), []byte("nagi-installer-v4\napp="+app+"\n"), 0644)
			archive := archiveBytes(t, map[string]string{"nagi": cliArtifact("v2.0.0"), "mihomo": "#!/bin/sh\necho fixture\n", "installer.sh": "#!/bin/sh\nexit 0\n", "Nagi.app/Contents/Info.plist": "plist", "Nagi.app/Contents/MacOS/NagiApp": "#!/bin/sh\nexit 0\n"}, "")
			upstream := releaseServer(t, archive, failure == "checksum", nil)
			var proxied int
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxied++
				if !r.URL.IsAbs() {
					t.Error("release request bypassed HTTP proxy")
				}
				target, _ := url.Parse(upstream.URL)
				r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
				r.RequestURI = ""
				response, err := http.DefaultTransport.RoundTrip(r)
				if err != nil {
					t.Error(err)
					http.Error(w, "forward failed", 500)
					return
				}
				defer response.Body.Close()
				for k, v := range response.Header {
					w.Header()[k] = v
				}
				w.WriteHeader(response.StatusCode)
				// Both fixture files are small and bounded.
				io.Copy(w, response.Body)
			}))
			defer proxy.Close()
			_, portText, _ := net.SplitHostPort(strings.TrimPrefix(proxy.URL, "http://"))
			port, _ := strconv.Atoi(portText)
			m.ReleaseBase = "http://unreachable.invalid/releases"
			var events []string
			m.Run = func(ctx context.Context, exe string, args ...string) ([]byte, error) {
				event := strings.Join(args, " ")
				if exe == m.Executable {
					events = append(events, event)
					switch event {
					case "--json status":
						return []byte(`{"ok":true,"data":{"running":true}}`), nil
					case "--json ports status":
						return []byte(fmt.Sprintf(`{"ok":true,"data":{"mixed":%d,"bind_address":"*"}}`, port)), nil
					default:
						t.Errorf("runtime changed during preparation: %s", event)
					}
				}
				if exe == "bash" && strings.Contains(event, "--verify") {
					return nil, errors.New("preflight failed")
				}
				return nil, fmt.Errorf("unexpected operation %s %s", exe, event)
			}
			_, err := m.Manage(context.Background(), "background-update", "")
			if err == nil || !strings.Contains(err.Error(), map[string]string{"checksum": "checksum mismatch", "preflight": "preflight failed"}[failure]) {
				t.Fatalf("%v", err)
			}
			if proxied != 2 || strings.Join(events, ",") != "--json status,--json ports status" {
				t.Fatalf("proxied=%d events=%v", proxied, events)
			}
		})
	}
}

func TestPreparedUpdateDoesNotDownloadAndRestoresPreQuitRunningState(t *testing.T) {
	m, _ := fixture(t)
	var requests []string
	server := releaseServer(t, standardArchive(t), false, &requests)
	artifact, cleanup, err := Fetch(context.Background(), server.URL, "linux-amd64", "")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	m.PreparedArtifact, m.EngineWasRunning = artifact, true
	m.ReleaseBase = "http://unreachable.invalid/releases"
	var restarted bool
	m.Run = func(ctx context.Context, exe string, args ...string) ([]byte, error) {
		if exe == m.Executable && strings.Join(args, " ") == "--json start" {
			restarted = true
		}
		return runProcess(ctx, exe, args...)
	}
	result, err := m.Manage(context.Background(), "update", "")
	if err != nil {
		t.Fatal(err)
	}
	if !restarted || result["engine_restarted"] != true || len(requests) != 2 {
		t.Fatalf("result=%v restarted=%t requests=%v", result, restarted, requests)
	}
}

func TestAppUpdateErrorIsRedactedAndConsumedWithoutInstallation(t *testing.T) {
	fixture(t)
	if err := SaveUpdateError(errors.New("installation failed: https://user:secret@example.com/private?token=abc")); err != nil {
		t.Fatal(err)
	}
	m := &Manager{OS: "darwin", Executable: "/missing/nagi"}
	result, err := m.Manage(context.Background(), "app-update-error", "")
	if err != nil || result["error"] != "installation failed: [redacted URL]" {
		t.Fatalf("%v %v", result, err)
	}
	result, err = m.Manage(context.Background(), "app-update-error", "")
	if err != nil || result["error"] != "" {
		t.Fatalf("%v %v", result, err)
	}
}

func TestDetachedHandoffKeepsLockAndReopensAfterFailure(t *testing.T) {
	m, prefix := fixture(t)
	root := filepath.Dir(prefix)
	app := filepath.Join(root, "Applications", "Nagi.app")
	os.MkdirAll(app, 0755)
	artifact := filepath.Join(root, "nagi-release-prepared", "artifact")
	os.MkdirAll(artifact, 0700)
	marker := filepath.Join(root, "app-running")
	os.WriteFile(marker, []byte("running"), 0600)
	tools := filepath.Join(root, "tools")
	os.MkdirAll(tools, 0755)
	os.WriteFile(filepath.Join(tools, "pgrep"), []byte("#!/bin/sh\ntest -e "+shellQuote(marker)+"\n"), 0755)
	opened := filepath.Join(root, "opened")
	opener := filepath.Join(tools, "open")
	os.WriteFile(opener, []byte("#!/bin/sh\nprintf '%s' \"$1\" > "+shellQuote(opened)+"\n"), 0755)
	capture := filepath.Join(root, "capture")
	os.WriteFile(m.Executable, []byte("#!/bin/sh\ntest -d \"$NAGI_UPDATE_ARTIFACT\" || exit 2\ntest -e /dev/fd/3 || exit 3\nprintf '%s\\n%s' \"$NAGI_UPDATE_ARTIFACT\" \"$NAGI_UPDATE_ENGINE_RUNNING\" > "+shellQuote(capture)+"\nexit 9\n"), 0755)
	t.Setenv("PATH", tools+":"+os.Getenv("PATH"))
	m.StartBackground = func(cmd *exec.Cmd) error {
		cmd.Args[2] = strings.ReplaceAll(cmd.Args[2], "/usr/bin/open", shellQuote(opener))
		return cmd.Start()
	}
	lockPath := filepath.Join(root, "maintenance.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	result, err := m.launchBackgroundUpdate(context.Background(), record{Prefix: prefix, App: app}, artifact, true, lock)
	if err != nil || result["scheduled"] != true {
		t.Fatalf("%v %v", result, err)
	}
	// The original CLI closes its descriptor without explicitly unlocking after
	// transfer. The waiting detached updater must retain the same lock.
	lock.Close()
	probe, err := os.OpenFile(lockPath, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	if err := syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		t.Fatal("maintenance lock lost during handoff")
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatal("installer started before App quit")
	}
	os.Remove(marker)
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, openErr := os.Stat(opened)
		_, artifactErr := os.Stat(artifact)
		if openErr == nil && os.IsNotExist(artifactErr) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("detached updater did not reopen App and clean artifacts")
		}
		time.Sleep(20 * time.Millisecond)
	}
	data, _ := os.ReadFile(capture)
	if string(data) != artifact+"\ntrue" {
		t.Fatalf("prepared handoff: %q", data)
	}
	data, _ = os.ReadFile(opened)
	if string(data) != app {
		t.Fatalf("reopened wrong App: %q", data)
	}
	result, err = consumeUpdateError()
	if err != nil || result["error"] == "" {
		t.Fatalf("background failure missing: %v %v", result, err)
	}
	if err := syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("completed updater retained lock: %v", err)
	}
	syscall.Flock(int(probe.Fd()), syscall.LOCK_UN)
}

func TestCancelledAuthorizationKillsDescendantsAndAllowsRetry(t *testing.T) {
	m, prefix := fixture(t)
	artifact := filepath.Join(filepath.Dir(prefix), "nagi-release-prepared", "artifact")
	os.MkdirAll(artifact, 0700)
	var lockPath = filepath.Join(filepath.Dir(prefix), "maintenance.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	// Mock a blocking authorization service, with descendants in the updater's
	// process group, without displaying a dialog or changing system files.
	m.StartBackground = func(cmd *exec.Cmd) error {
		cmd.Args[2] = strings.ReplaceAll(cmd.Args[2], "sudo -v < /dev/null 3>&-", "sleep 60")
		return cmd.Start()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	result, err := m.launchBackgroundUpdate(ctx, record{Prefix: "/usr/local", App: "/Applications/Nagi.app"}, artifact, false, lock)
	if err == nil || result != nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("%v %v", result, err)
	}
	// Parent cleanup closes the final descriptor; no authorization descendant may
	// retain it and prevent an immediate retry.
	lock.Close()
	probe, err := os.OpenFile(lockPath, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	if err := syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("cancelled authorization retained maintenance lock: %v", err)
	}
	syscall.Flock(int(probe.Fd()), syscall.LOCK_UN)
}
