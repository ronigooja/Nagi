package scripts

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run a native executable fixture so Linux's /proc/self/exe tracks the same
// inode relocation as a menu process. A shell-script fixture cannot do this.
func init() {
	if os.Getenv("NAGI_INSTALLER_PROCESS_FIXTURE") != "1" {
		return
	}
	version := func() {
		exe, err := os.Executable()
		if err != nil {
			panic(err)
		}
		binary := os.Getenv("NAGI_MIHOMO_BIN")
		if binary == "" {
			binary = filepath.Join(filepath.Dir(exe), "mihomo")
		}
		out, err := exec.Command(binary, "-v").Output()
		if err != nil {
			out = []byte("unavailable")
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "nagi": "fixture", "mihomo_commit": "fixture", "mihomo": strings.TrimSpace(string(out))})
	}
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "hold" {
		fmt.Println("ready")
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			version()
		}
	} else if len(args) == 1 && args[0] == "__default-profile" {
		fmt.Println("mode: direct")
	} else if len(args) >= 2 && args[0] == "--json" {
		switch args[1] {
		case "version":
			if os.Getenv("NAGI_INSTALLER_FAIL_VERSION") == "1" && strings.HasSuffix(os.Args[0], "/bin/nagi") {
				os.Exit(17)
			}
			version()
		case "status":
			fmt.Println(`{"ok":true,"running":false}`)
		case "service":
			fmt.Println(`{"ok":true,"installed":false,"active":false}`)
		default:
			os.Exit(2)
		}
	} else {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestUpdateKeepsRunningExecutableAtInstalledPath(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollback=%t", rollback), func(t *testing.T) {
			root := t.TempDir()
			prefix := filepath.Join(root, "prefix")
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			fixture, err := os.ReadFile(exe)
			if err != nil {
				t.Fatal(err)
			}
			artifact := filepath.Join(root, "artifact")
			if err := os.MkdirAll(artifact, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(artifact, "nagi"), fixture, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(artifact, "mihomo"), []byte("#!/bin/sh\necho fixture-engine\n"), 0755); err != nil {
				t.Fatal(err)
			}
			env := append(os.Environ(), "NAGI_INSTALLER_PROCESS_FIXTURE=1", "NAGI_MIHOMO_BIN=", "SUDO_USER=", "HOME="+filepath.Join(root, "home"), "XDG_CONFIG_HOME="+filepath.Join(root, "config"))
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			install := func(action string, fail bool) ([]byte, error) {
				cmd := exec.CommandContext(ctx, "bash", "installer.sh", action, "--artifact-dir", artifact, "--prefix", prefix)
				failVersion := "0"
				if fail {
					failVersion = "1"
				}
				cmd.Env = append(env, "NAGI_INSTALLER_FAIL_VERSION="+failVersion)
				return cmd.CombinedOutput()
			}
			if out, err := install("install", false); err != nil {
				t.Fatalf("install: %v\n%s", err, out)
			}
			menu := exec.CommandContext(ctx, filepath.Join(prefix, "bin", "nagi"), "hold")
			menu.Env = env
			stdin, err := menu.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := menu.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := menu.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { stdin.Close(); menu.Wait() }()
			scanner := bufio.NewScanner(stdout)
			if !scanner.Scan() || scanner.Text() != "ready" {
				t.Fatal("running CLI did not become ready")
			}
			out, err := install("update", rollback)
			if rollback && (err == nil || !strings.Contains(string(out), "installed nagi failed its version check")) {
				t.Fatalf("expected rollback: %v\n%s", err, out)
			}
			if !rollback && err != nil {
				t.Fatalf("update: %v\n%s", err, out)
			}
			fmt.Fprintln(stdin, "version")
			if !scanner.Scan() {
				t.Fatal("running CLI did not report its version after update")
			}
			var result struct{ Mihomo string }
			if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Mihomo != "fixture-engine" {
				t.Fatalf("running CLI lost its installed mihomo path: %s", scanner.Text())
			}
		})
	}
}
