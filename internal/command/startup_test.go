package command

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/engine"
)

type startupBackend struct {
	dnsStartupBackend
	stopContextErr error
	stopErr        error
}

func (b *startupBackend) Stop(ctx context.Context) (engine.Status, error) {
	b.stopped = true
	b.stopContextErr = ctx.Err()
	return engine.Status{}, b.stopErr
}

func startupProfile(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "work.yaml")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStartupWaitsForCompletedActivation(t *testing.T) {
	for _, config := range []string{"mixed-port: 17890\n", "mixed-port: 0\n", "mode: direct\n"} {
		t.Run(strings.TrimSpace(config), func(t *testing.T) {
			backend := &startupBackend{}
			reads := 0
			client := control.NewWithBridge(func(_ context.Context, method, path string, _, out any) error {
				if method != "GET" || path != "/configs" {
					t.Fatalf("unexpected request: %s %s", method, path)
				}
				reads++
				switch reads {
				case 1:
					return errors.New("controller temporarily unavailable")
				case 2:
					return json.Unmarshal([]byte(`{"mixed-port":0,"activation_id":""}`), out)
				case 3:
					// A listening port alone does not confirm TUN/provider readiness.
					return json.Unmarshal([]byte(`{"mixed-port":17890,"activation_id":""}`), out)
				default:
					return json.Unmarshal([]byte(`{"mixed-port":17890,"activation_id":"0123456789abcdef0123456789abcdef"}`), out)
				}
			}, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			result, err := startAndVerify(ctx, backend, client, startupProfile(t, config), "work")
			if err != nil || backend.stopped || reads != 4 || !result.(engine.Status).Running {
				t.Fatalf("result=%v err=%v stopped=%v reads=%d", result, err, backend.stopped, reads)
			}
		})
	}
}

func TestStartupCompletedPortMismatchStopsEngine(t *testing.T) {
	backend := &startupBackend{}
	client := control.NewWithBridge(func(_ context.Context, _, _ string, _, out any) error {
		return json.Unmarshal([]byte(`{"mixed-port":0,"activation_id":"0123456789abcdef0123456789abcdef"}`), out)
	}, nil)
	_, err := startAndVerify(context.Background(), backend, client, startupProfile(t, "mixed-port: 17890\n"), "work")
	if err == nil || !strings.Contains(err.Error(), "reports 0") || !backend.stopped || backend.stopContextErr != nil {
		t.Fatalf("err=%v backend=%+v", err, backend)
	}
}

func TestStartupUnconfirmedActivationCleansUpAfterDeadlineOrCancellation(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "cancellation"}[cancelEarly], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			stopErr := errors.New("shutdown failed")
			backend := &startupBackend{stopErr: stopErr}
			client := control.NewWithBridge(func(_ context.Context, _, _ string, _, out any) error {
				if cancelEarly {
					cancel()
				}
				return json.Unmarshal([]byte(`{"mixed-port":17890,"activation_id":""}`), out)
			}, nil)
			_, err := startAndVerify(ctx, backend, client, startupProfile(t, "mixed-port: 17890\n"), "work")
			cause := context.DeadlineExceeded
			if cancelEarly {
				cause = context.Canceled
			}
			if !errors.Is(err, cause) || !errors.Is(err, stopErr) || !backend.stopped || backend.stopContextErr != nil {
				t.Fatalf("err=%v backend=%+v", err, backend)
			}
		})
	}
}
