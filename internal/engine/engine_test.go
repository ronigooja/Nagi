package engine

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

func TestHelperProcess(t *testing.T) {
	if os.Getenv("NAGI_ENGINE_HELPER") != "1" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	for _, arg := range args {
		if arg == "-t" {
			for j, value := range args {
				if value == "-f" && j+1 < len(args) {
					data, _ := os.ReadFile(args[j+1])
					if strings.Contains(string(data), "bad") {
						os.Exit(1)
					}
				}
			}
			os.Exit(0)
		}
	}
	var socket string
	for i, arg := range args {
		if arg == "-ext-ctl-unix" && i+1 < len(args) {
			socket = args[i+1]
		}
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		os.Exit(2)
	}
	defer listener.Close()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	<-signals
	os.Exit(0)
}

func TestLifecycle(t *testing.T) {
	t.Setenv("NAGI_ENGINE_HELPER", "1")
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(dir, "fake-mihomo")
	if err := os.WriteFile(wrapper, []byte(fmt.Sprintf("#!/bin/sh\nexec '%s' -test.run=^TestHelperProcess$ -- \"$@\"\n", strings.ReplaceAll(executable, "'", "'\\''"))), 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(config, []byte("mixed-port: 17890\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(dir, "runtime")
	paths := nagiruntime.Paths{RuntimeDir: runDir, SocketPath: filepath.Join(runDir, "mihomo.sock"), PIDPath: filepath.Join(runDir, "mihomo.pid"), LockPath: filepath.Join(runDir, "mihomo.lock"), LogPath: filepath.Join(runDir, "logs", "mihomo.log")}
	manager, err := New(Options{Binary: wrapper, ConfigPath: config, Paths: paths, StartupTimeout: 3 * time.Second, StopTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	before, err := manager.Status(ctx)
	if err != nil || before.Running {
		t.Fatalf("before: %+v %v", before, err)
	}
	started, err := manager.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(ctx)
	if !started.Running || started.PID <= 0 {
		t.Fatalf("start: %+v", started)
	}
	if _, err := manager.Start(ctx); err != ErrAlreadyRunning {
		t.Fatalf("duplicate start: %v", err)
	}
	info, err := os.Stat(paths.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("socket permissions: %v", info.Mode().Perm())
	}
	stopped, err := manager.Stop(ctx)
	if err != nil || stopped.Running {
		t.Fatalf("stop: %+v %v", stopped, err)
	}
	if _, err := os.Stat(paths.PIDPath); !os.IsNotExist(err) {
		t.Fatalf("PID file remains: %v", err)
	}
	if _, err := manager.Stop(ctx); err != ErrNotRunning {
		t.Fatalf("second stop: %v", err)
	}
}

func TestValidationRejectsInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(config, []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := New(Options{Binary: binary, ConfigPath: config})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Validate(context.Background()); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestStatusWithoutBinary(t *testing.T) {
	dir := t.TempDir()
	paths := nagiruntime.Paths{RuntimeDir: dir, SocketPath: filepath.Join(dir, "mihomo.sock"), PIDPath: filepath.Join(dir, "mihomo.pid"), LockPath: filepath.Join(dir, "mihomo.lock"), LogPath: filepath.Join(dir, "mihomo.log")}
	manager, err := New(Options{Binary: filepath.Join(dir, "missing"), ConfigPath: filepath.Join(dir, "missing.yaml"), Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	status, err := manager.Status(context.Background())
	if err != nil || status.Running {
		t.Fatalf("status: %+v %v", status, err)
	}
	if _, err := manager.Stop(context.Background()); err != ErrNotRunning {
		t.Fatalf("stop: %v", err)
	}
}

func TestRecoverRemovesStaleRuntimeState(t *testing.T) {
	root := t.TempDir()
	paths := nagiruntime.Paths{RuntimeDir: filepath.Join(root, "run"), SocketPath: filepath.Join(root, "run", "sock"), PIDPath: filepath.Join(root, "run", "pid"), LockPath: filepath.Join(root, "run", "lock"), LogPath: filepath.Join(root, "run", "log")}
	if err := os.MkdirAll(paths.RuntimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.PIDPath, []byte("999999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.SocketPath, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{Binary: "mihomo", ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	status, err := m.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.UnexpectedExit || status.StalePID != 999999 {
		t.Fatalf("status=%+v", status)
	}
	if _, err = os.Stat(paths.PIDPath); !os.IsNotExist(err) {
		t.Fatalf("pid remains: %v", err)
	}
	if _, err = os.Stat(paths.SocketPath); !os.IsNotExist(err) {
		t.Fatalf("socket remains: %v", err)
	}
}
