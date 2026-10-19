package command

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/control"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

func TestCaptureCheckStopsAtFirstUnmetPrerequisite(t *testing.T) {
	for _, stage := range []string{"engine", "rewrite", "capture", "tun", "ca", "trust", "unknown-trust", "ready"} {
		t.Run(stage, func(t *testing.T) {
			p, engine, children := rewriteCoordinatorFixture(t)
			var requests []string
			trustChecks := 0
			rewriteTrustAssessment = func([]byte) string {
				trustChecks++
				if stage == "trust" {
					return "untrusted"
				}
				if stage == "unknown-trust" {
					return "unknown"
				}
				return "trusted"
			}
			if stage == "ca" {
				if err := os.RemoveAll(filepath.Join(p.ConfigDir, "request-rewrite", "ca")); err != nil {
					t.Fatal(err)
				}
			}
			engine.active.TUNRunning = stage != "tun"
			rewriteClient = func(nagiruntime.Paths) (*control.Client, error) {
				return control.NewWithBridge(func(ctx context.Context, method, path string, body, out any) error {
					if method != "GET" {
						t.Fatalf("check mutated runtime: %s %s", method, path)
					}
					requests = append(requests, path)
					switch path {
					case "/version":
						if stage == "engine" {
							return errors.New("private fixture credential")
						}
						return nil
					case "/request-rewrite/capabilities":
						if stage == "rewrite" {
							return errors.New("private fixture credential")
						}
						if err := engine.call(ctx, method, path, body, out); err != nil {
							return err
						}
						out.(*control.RewriteCapabilities).CaptureProtocol = 1
						if stage == "capture" {
							out.(*control.RewriteCapabilities).CaptureProtocol = 0
						}
						return nil
					default:
						return engine.call(ctx, method, path, body, out)
					}
				}, nil), nil
			}
			result := captureReadiness(context.Background(), p)
			codes := map[string]string{"engine": "rewrite_unavailable", "rewrite": "rewrite_capability_missing", "capture": "capture_capability_missing", "tun": "rewrite_tun_required", "ca": "rewrite_ca_required", "trust": "rewrite_ca_trust_required", "unknown-trust": "rewrite_ca_trust_required", "ready": ""}
			if result.Code != codes[stage] || result.Ready != (stage == "ready") || strings.Contains(result.Message, "private fixture") {
				t.Fatalf("%+v", result)
			}
			expected := []string{"/version"}
			if stage != "engine" {
				expected = append(expected, "/request-rewrite/capabilities")
			}
			if stage != "engine" && stage != "rewrite" && stage != "capture" {
				expected = append(expected, "/request-rewrite/status")
			}
			if !reflect.DeepEqual(requests, expected) {
				t.Fatalf("later stages inspected: %v", requests)
			}
			expectedTrust := 0
			if stage == "trust" || stage == "unknown-trust" || stage == "ready" {
				expectedTrust = 1
			}
			if trustChecks != expectedTrust {
				t.Fatalf("trust checks = %d", trustChecks)
			}
			if len(*children) != 0 {
				t.Fatal("check started a child")
			}
			if _, err := os.Lstat(captureSocket(p)); !os.IsNotExist(err) {
				t.Fatalf("recorder created: %v", err)
			}
		})
	}
}

func TestCaptureCheckCLIAndAbsentRecorder(t *testing.T) {
	p := isolatedTestPaths(t, t.TempDir())
	for _, root := range []string{"rewrite", "request-rewrite"} {
		value := rewriteAliasOK(t, root, "", "capture", "check")
		readiness := value["readiness"].(map[string]any)
		if readiness["ready"] != false || readiness["message"] == "" {
			t.Fatal(value)
		}
		value = rewriteAliasOK(t, root, "", "capture", "status")
		if value["status"] != nil {
			t.Fatal(value)
		}
		code, output, diagnostic := rewriteCLIRun(t, "", root, "capture", "check")
		if code != 0 || diagnostic != "" || !strings.Contains(output, "ready") {
			t.Fatal(code, output, diagnostic)
		}
	}
	if _, err := os.Lstat(captureSocket(p)); !os.IsNotExist(err) {
		t.Fatal("read-only command created recorder")
	}
	for _, args := range [][]string{{"rewrite", "capture", "check", "extra"}, {"rewrite", "capture", "check", "--profile", "work"}} {
		if code, _, _ := rewriteCLIRun(t, "", args...); code != 2 {
			t.Fatal(args, code)
		}
	}
	invalidTestConfigDir(t, t.TempDir())
	for _, root := range []string{"rewrite", "request-rewrite"} {
		code, output, diagnostic := rewriteCLIRun(t, "", root, "capture", "check", "--help")
		if code != 0 || diagnostic != "" || !strings.Contains(output, "first unmet") {
			t.Fatal(code, output, diagnostic)
		}
	}
}
