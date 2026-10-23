package installation

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/scripts"
)

func cliArtifact(version string) string {
	return `#!/bin/sh
if [ "${1:-}" = __install-transaction ]; then
  if [ -n "${NAGI_TEST_TX_LOG:-}" ] && [ "${2:-}" = --locked ]; then
    printf '%s\n' "${3:-}" >> "$NAGI_TEST_TX_LOG"
  fi
  if [ "${NAGI_TEST_COMMIT_UNCERTAIN:-}" = 1 ] && [ "${3:-}" = commit ]; then
    exit 5
  fi
  if [ "${NAGI_TEST_FAIL_COMMIT_CLEANUP:-}" = 1 ] && [ "${3:-}" = commit ]; then
    "$NAGI_TEST_INSTALL_HELPER" "$@" || exit $?
    exit 4
  fi
  if [ "${NAGI_TEST_FAIL_ACTIVATE:-}" = 1 ] && [ "${3:-}" = activate ]; then
    "$NAGI_TEST_INSTALL_HELPER" "$@" || exit $?
    exit 1
  fi
  exec "$NAGI_TEST_INSTALL_HELPER" "$@"
fi
case "$*" in
  '--json version') echo '{"ok":true,"data":{"nagi":"` + version + `","mihomo_commit":"test"}}' ;;
  __default-profile) echo 'mixed-port: 17890' ;;
  '--json status') echo '{"ok":true,"data":{"running":false}}' ;;
  '--json service status') echo '{"ok":true,"data":{"installed":false,"active":false}}' ;;
  '--json privileged-helper status') echo '{"ok":true,"data":{"installed":false,"selected_backend":"user"}}' ;;
  *) echo '{"ok":true,"data":{}}' ;;
esac
`
}

func archiveBytes(t *testing.T, files map[string]string, link string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := gzip.NewWriter(&buf)
	w := tar.NewWriter(z)
	for name, content := range files {
		if err := w.WriteHeader(&tar.Header{Name: name, Size: int64(len(content)), Mode: 0755}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if link != "" {
		if err := w.WriteHeader(&tar.Header{Name: "escape", Typeflag: tar.TypeSymlink, Linkname: link}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func releaseServer(t *testing.T, archive []byte, corrupt bool, requests *[]string) *httptest.Server {
	t.Helper()
	hash := sha256.Sum256(archive)
	digest := hex.EncodeToString(hash[:])
	if corrupt {
		digest = strings.Repeat("0", 64)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests != nil {
			*requests = append(*requests, r.URL.Path)
		}
		if strings.HasSuffix(r.URL.Path, "SHA256SUMS") {
			fmt.Fprintf(w, "%s  nagi-linux-amd64.tar.gz\n%s  nagi-macos-arm64.tar.gz\n", digest, digest)
		} else {
			w.Write(archive)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

var transactionHelperBuild struct {
	sync.Once
	path string
	err  error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if transactionHelperBuild.path != "" {
		os.RemoveAll(filepath.Dir(transactionHelperBuild.path))
	}
	os.Exit(code)
}

func useTransactionHelper(t *testing.T) {
	t.Helper()
	transactionHelperBuild.Do(func() {
		dir, err := os.MkdirTemp("", "nagi-install-test-helper-")
		if err != nil {
			transactionHelperBuild.err = err
			return
		}
		transactionHelperBuild.path = filepath.Join(dir, "nagi")
		cmd := exec.Command("go", "build", "-o", transactionHelperBuild.path, "../../cmd/nagi")
		if output, err := cmd.CombinedOutput(); err != nil {
			transactionHelperBuild.err = fmt.Errorf("build transaction helper: %s: %w", output, err)
		}
	})
	if transactionHelperBuild.err != nil {
		t.Fatal(transactionHelperBuild.err)
	}
	t.Setenv("NAGI_TEST_INSTALL_HELPER", transactionHelperBuild.path)
}

func fixture(t *testing.T) (*Manager, string) {
	t.Helper()
	useTransactionHelper(t)
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("SUDO_USER", "")
	prefix := filepath.Join(root, "prefix")
	for _, dir := range []string{"bin", "share/nagi"} {
		if err := os.MkdirAll(filepath.Join(prefix, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	cli := cliArtifact("v1.0.0")
	engine := "#!/bin/sh\necho 'mihomo fixture'\n"
	for name, content := range map[string]string{"nagi": cli, "mihomo": engine} {
		if err := os.WriteFile(filepath.Join(prefix, "bin", name), []byte(content), 0755); err != nil {
			t.Fatal(err)
		}
	}
	nagiHash, engineHash := sha256.Sum256([]byte(cli)), sha256.Sum256([]byte(engine))
	state := fmt.Sprintf("nagi-installer-v3\napp=\nnagi_sha256=%x\nmihomo_sha256=%x\napp_sha256=\n", nagiHash, engineHash)
	if err := os.WriteFile(filepath.Join(prefix, "share/nagi/install-state"), []byte(state), 0644); err != nil {
		t.Fatal(err)
	}
	return &Manager{OS: "linux", Arch: "amd64", Executable: filepath.Join(prefix, "bin/nagi"), Version: "v1.0.0", Run: runProcess}, prefix
}

func standardArchive(t *testing.T) []byte {
	return archiveBytes(t, map[string]string{"nagi": cliArtifact("v2.0.0"), "mihomo": "#!/bin/sh\necho 'mihomo new'\n", "installer.sh": scripts.Installer}, "")
}

func TestFetchStopsWhenContextIsCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _, err := Fetch(ctx, server.URL, "linux-amd64", "")
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Fetch error = %v, want context deadline exceeded", err)
	}
}

func TestVerifiedUpdateRestoresServicePreferenceAndRestarts(t *testing.T) {
	m, prefix := fixture(t)
	if err := os.WriteFile(filepath.Join(prefix, "share/nagi/install-state"), []byte("nagi-installer-v4\napp=\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m.ReleaseBase = releaseServer(t, standardArchive(t), false, nil).URL
	var events []string
	m.Run = func(ctx context.Context, exe string, args ...string) ([]byte, error) {
		if exe == m.Executable {
			event := strings.Join(args, " ")
			events = append(events, event)
			switch event {
			case "--json service status":
				return []byte(`{"ok":true,"data":{"installed":true,"enabled":false}}`), nil
			case "--json status":
				return []byte(`{"ok":true,"data":{"running":true}}`), nil
			case "--json stop":
				return []byte(`{"ok":false,"error":{"code":"not_running"}}`), errors.New("not running")
			}
		}
		return runProcess(ctx, exe, args...)
	}
	result, err := m.Manage(context.Background(), "update", "")
	if err != nil {
		t.Fatal(err)
	}
	if result["engine_restarted"] != true {
		t.Fatalf("%v", result)
	}
	if got := strings.Join(events, ","); got != "--json service status,--json status,--json quit,--json stop,--json service uninstall,--json service install,--json startup disable,--json start" {
		t.Fatalf("events: %s", got)
	}
	data, err := os.ReadFile(filepath.Join(prefix, "bin/nagi"))
	if err != nil || !strings.Contains(string(data), "v2.0.0") {
		t.Fatalf("update: %s %v", data, err)
	}
	profile := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "nagi/profiles/default.yaml")
	if runtime.GOOS == "darwin" {
		profile = filepath.Join(os.Getenv("HOME"), "Library/Application Support/Nagi/config/profiles/default.yaml")
	}
	if _, err := os.Stat(profile); err != nil {
		t.Fatal(err)
	}
}

func TestBadChecksumAndPreflightLeaveEngineUntouched(t *testing.T) {
	for _, kind := range []string{"checksum", "missing-app"} {
		t.Run(kind, func(t *testing.T) {
			m, prefix := fixture(t)
			m.ReleaseBase = releaseServer(t, standardArchive(t), kind == "checksum", nil).URL
			if kind == "missing-app" {
				path := filepath.Join(prefix, "share/nagi/install-state")
				data, _ := os.ReadFile(path)
				os.WriteFile(path, bytes.Replace(data, []byte("app=\n"), []byte("app=/Applications/Nagi.app\n"), 1), 0644)
			}
			called := false
			m.Run = func(ctx context.Context, exe string, args ...string) ([]byte, error) {
				if exe == m.Executable {
					called = true
				}
				return runProcess(ctx, exe, args...)
			}
			if _, err := m.Manage(context.Background(), "update", ""); err == nil {
				t.Fatal("invalid update accepted")
			}
			if called {
				t.Fatal("engine/service mutated before successful verification")
			}
		})
	}
}

func TestUpdateReplacesModifiedRecordedExecutable(t *testing.T) {
	m, prefix := fixture(t)
	m.ReleaseBase = releaseServer(t, standardArchive(t), false, nil).URL
	if err := os.WriteFile(m.Executable, []byte(cliArtifact("edited")), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Manage(context.Background(), "update", ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(prefix, "bin/nagi"))
	if err != nil || !strings.Contains(string(data), "v2.0.0") {
		t.Fatalf("updated executable: %s %v", data, err)
	}
	state, err := os.ReadFile(filepath.Join(prefix, "share/nagi/install-state"))
	if err != nil || string(state) != "nagi-installer-v4\napp=\n" {
		t.Fatalf("migrated installer record: %s %v", state, err)
	}
}

func TestFailedTransactionRestoresServiceButLeavesEngineStopped(t *testing.T) {
	m, _ := fixture(t)
	m.ReleaseBase = releaseServer(t, standardArchive(t), false, nil).URL
	var events []string
	m.Run = func(ctx context.Context, exe string, args ...string) ([]byte, error) {
		event := strings.Join(args, " ")
		if exe == "bash" && !strings.Contains(event, "--verify") {
			return nil, errors.New("injected transaction failure")
		}
		if exe == m.Executable {
			events = append(events, event)
			if event == "--json service status" {
				return []byte(`{"ok":true,"data":{"installed":true,"enabled":true}}`), nil
			}
			if event == "--json status" {
				return []byte(`{"ok":true,"data":{"running":true}}`), nil
			}
		}
		return runProcess(ctx, exe, args...)
	}
	if _, err := m.Manage(context.Background(), "update", ""); err == nil || !strings.Contains(err.Error(), "remains stopped") {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(strings.Join(events, ","), "service install") || strings.Contains(strings.Join(events, ","), "--json start") {
		t.Fatalf("events %v", events)
	}
}

func TestUninstallUsesNoNetworkAndPreservesUserData(t *testing.T) {
	m, prefix := fixture(t)
	m.ReleaseBase = "invalid; no network should be used"
	personal := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "nagi/profiles/work.yaml")
	os.MkdirAll(filepath.Dir(personal), 0700)
	os.WriteFile(personal, []byte("personal profile"), 0600)
	if _, err := m.Manage(context.Background(), "uninstall", ""); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"bin/nagi", "bin/mihomo", "share/nagi/install-state"} {
		if _, err := os.Stat(filepath.Join(prefix, path)); !os.IsNotExist(err) {
			t.Fatalf("remaining %s", path)
		}
	}
	if data, err := os.ReadFile(personal); err != nil || string(data) != "personal profile" {
		t.Fatal("personal profile removed")
	}
}

func TestFetchRejectsTraversalLinksAndDuplicateFiles(t *testing.T) {
	for _, kind := range []string{"traversal", "symlink", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			files := map[string]string{"nagi": "exe", "mihomo": "exe", "installer.sh": "exe"}
			link := ""
			switch kind {
			case "traversal":
				files["../escaped"] = "bad"
			case "symlink":
				link = "/tmp"
			case "duplicate":
				files["./nagi"] = "duplicate"
			}
			server := releaseServer(t, archiveBytes(t, files, link), false, nil)
			if _, cleanup, err := Fetch(context.Background(), server.URL, "linux-amd64", "v1.0.0"); err == nil {
				cleanup()
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestAppVersionAndPlatformValidation(t *testing.T) {
	m, _ := fixture(t)
	if _, err := m.Manage(context.Background(), "app-install", ""); err == nil || !strings.Contains(err.Error(), "macOS") {
		t.Fatalf("%v", err)
	}
	for _, tag := range []string{"main", "../../v1.0.0", "--terminal", "v1", "v1.0.0\n"} {
		if ValidTag(tag) {
			t.Fatalf("accepted %q", tag)
		}
	}
	for _, tag := range []string{"v1.0.0", "1.0.0", "v1.0.0-rc.1"} {
		if !ValidTag(tag) {
			t.Fatalf("rejected %q", tag)
		}
	}
}

func TestTerminalLauncherQuotesPathsAndOnlySchedules(t *testing.T) {
	m := &Manager{OS: "darwin", Executable: "/usr/local/bin/nagi"}
	m.Run = func(_ context.Context, exe string, args ...string) ([]byte, error) {
		if exe != "/usr/bin/osascript" || !strings.Contains(args[2], "update && open") || !strings.Contains(args[2], "pgrep -x NagiApp") || !strings.Contains(args[2], "60") {
			t.Fatalf("%s %v", exe, args)
		}
		return nil, nil
	}
	result, err := m.launchTerminal(context.Background(), record{App: "/Applications/Nagi.app"})
	if err != nil || result["scheduled"] != true {
		t.Fatalf("%v %v", result, err)
	}
	if shellQuote("it's $(secret)") != `'it'"'"'s $(secret)'` {
		t.Fatal("unsafe shell quoting")
	}
}

func TestNativeUpdateAuthorizationUsesSystemDialog(t *testing.T) {
	if !strings.Contains(nativeUpdateSudo, "with administrator privileges") || !strings.Contains(nativeUpdateSudo, "Authorization") {
		t.Fatal("native authorization adapter is not using macOS authorization")
	}
	if strings.Contains(nativeUpdateSudo, "display dialog") || strings.Contains(nativeUpdateSudo, "text returned") {
		t.Fatal("native authorization adapter must not collect a password")
	}
	if !strings.Contains(nativeUpdateSudo, "session_uid=$(/usr/bin/id -u)") ||
		!strings.Contains(nativeUpdateSudo, "SUDO_UID=$session_uid") ||
		strings.Contains(nativeUpdateSudo, `"SUDO_UID=$(/usr/bin/id -u)"`) {
		t.Fatal("native authorization adapter must preserve the pre-authorization session user")
	}
}

func TestNativeUpdateAuthorizationSharesWorker(t *testing.T) {
	bridge := t.TempDir()
	// Simulate Authorization Services without prompting or requiring root.
	authorizer := filepath.Join(bridge, "authorize")
	if err := os.WriteFile(authorizer, []byte("#!/bin/bash\necho launch >> \"$(dirname \"$0\")/launches\"\nexec /bin/bash -c \"$3\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	adapter := strings.ReplaceAll(nativeUpdateSudo, "/usr/bin/osascript", shellQuote(authorizer))
	for name, source := range map[string]string{"sudo": adapter, "worker": nativeUpdateWorker} {
		if err := os.WriteFile(filepath.Join(bridge, name), []byte(source), 0700); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	script := `export NAGI_UPDATE_OWNER_PID=$$
trap 'rm -f "$bridge/alive"' EXIT
"$bridge/sudo" -v < /dev/null || exit 1
"$bridge/sudo" -n true || exit 2
printf 'input bytes' | "$bridge/sudo" /usr/bin/tee "$bridge/copied" || exit 3
"$bridge/sudo" /bin/bash -c 'echo diagnostic >&2; exit 7' < /dev/null
[[ $? == 7 ]] || exit 4
"$bridge/sudo" /bin/bash -c 'printf "%s" "$SUDO_UID"' < /dev/null || exit 5
rm -f "$bridge/alive"
for ((i=0; i<50; i++)); do [[ -e "$bridge/ended" ]] && exit 0; sleep 0.1; done
exit 6
`
	cmd := exec.CommandContext(ctx, "/bin/bash", "-c", script)
	cmd.Env = append(os.Environ(), "bridge="+bridge)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("authorization session: %v: %s", err, out)
	}
	launches, _ := os.ReadFile(filepath.Join(bridge, "launches"))
	if string(launches) != "launch\n" {
		t.Fatalf("expected one authorization, got %q", launches)
	}
	input, _ := os.ReadFile(filepath.Join(bridge, "copied"))
	if string(input) != "input bytes" || !bytes.Contains(out, []byte("diagnostic")) {
		t.Fatalf("stdin/stderr lost: %q %q", input, out)
	}
}
