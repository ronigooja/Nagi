package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/ronigooja/Nagi/internal/capture"
	"github.com/ronigooja/Nagi/internal/control"
	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
	child "github.com/ronigooja/Nagi/internal/requestrewriteservice"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"os"
	"strings"
	"testing"
	"time"
)

func startTestCaptureRecorder(t *testing.T, p nagiruntime.Paths) {
	t.Helper()
	if e := p.EnsureRuntime(); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var serveErr error
	go func() {
		serveErr = capture.Serve(ctx, captureSocket(p), os.Geteuid())
		close(done)
	}()
	t.Cleanup(func() { cancel(); <-done })
	var lastErr error
	for i := 0; i < 100; i++ {
		select {
		case <-done:
			t.Fatalf("recorder exited before becoming ready: %v", serveErr)
		default:
		}
		if _, e := capture.Call(context.Background(), captureSocket(p), capture.Request{Action: "status"}); e == nil {
			return
		} else {
			lastErr = e
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("recorder not ready: %v", lastErr)
}
func TestCaptureCLIInvalidArgumentsBeforeRuntime(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	for _, root := range []string{"rewrite", "request-rewrite"} {
		for _, args := range [][]string{{"capture"}, {"capture", "body", "r", "request_original", "-1"}, {"capture", "body", "r", "bad", "0"}, {"capture", "body", "r", "request_original", "0", "262145"}, {"capture", "pause"}, {"capture", "export", "-", "xml"}, {"capture", "export", "-", "--json"}, {"capture", "start", "--profile", "work"}, {"capture", "fixture", "r", "bad"}, {"draft", "save"}} {
			code, out, err := rewriteCLIRun(t, "", append([]string{root}, args...)...)
			if code != 2 || out != "" || !strings.Contains(err, "usage") && !strings.Contains(err, "Usage") {
				t.Fatalf("%v: %d %s %s", args, code, out, err)
			}
		}
	}
}
func TestCaptureCLIOfflineRawArchiveJourney(t *testing.T) {
	p := isolatedTestPaths(t, t.TempDir())
	startTestCaptureRecorder(t, p)
	archive, e := os.ReadFile("../capture/testdata/native-v1.ndjson")
	if e != nil {
		t.Fatal(e)
	}
	v := rewriteAliasOK(t, "rewrite", string(archive), "capture", "import", "-")
	status := v["status"].(map[string]any)
	if status["state"] != "stopped" || status["read_only"] != true {
		t.Fatal(v)
	}
	v = rewriteAliasOK(t, "request-rewrite", "", "capture", "list")
	records := v["records"].([]any)
	if len(records) == 0 {
		t.Fatal(v)
	}
	record := records[0].(map[string]any)
	id := record["id"].(string)
	for _, root := range []string{"rewrite", "request-rewrite"} {
		code, out, err := rewriteCLIRun(t, "", root, "capture", "show", id)
		if code != 0 || err != "" || !strings.Contains(out, "2099-01-01 12:34") && !strings.Contains(out, "started") {
			t.Fatal(code, out, err)
		}
		v = rewriteAliasOK(t, root, "", "capture", "show", id)
		if v["sensitive"] != true {
			t.Fatal(v)
		}
	}
	var exported bytes.Buffer
	var errOut bytes.Buffer
	if code := RunWithInput([]string{"rewrite", "capture", "export", "-"}, strings.NewReader(""), &exported, &errOut, "test", "test"); code != 0 {
		t.Fatal(code, errOut.String())
	}
	if !strings.Contains(exported.String(), `"type":"end"`) {
		t.Fatal(exported.String())
	}
	path := t.TempDir() + "/raw.ndjson"
	if code, out, err := rewriteCLIRun(t, "", "rewrite", "capture", "export", path); code != 0 || err != "" {
		t.Fatal(code, out, err)
	}
	info, e := os.Stat(path)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, e)
	}
	code, _, _ := rewriteCLIRun(t, "", "rewrite", "capture", "export", path)
	if code != 1 {
		t.Fatal("overwrote archive")
	}
	if e = capture.Shutdown(context.Background(), captureSocket(p)); e != nil {
		t.Fatal(e)
	}
}
func TestCaptureOnlyCoordinatorRetainsDisabledIntentAndVersion(t *testing.T) {
	p, engine, children := rewriteCoordinatorFixture(t)
	startTestCaptureRecorder(t, p)
	snapshot := rewriteEnabledFixture(t)
	doc := snapshot.Document()
	doc.Enabled = false
	snapshot, e := rewrite.Compile(doc)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = (rewrite.Store{ConfigDir: p.ConfigDir, Profile: "work"}).Save(context.Background(), snapshot); e != nil {
		t.Fatal(e)
	}
	backend, e := readBackend(p)
	if e != nil {
		t.Fatal(e)
	}
	r, e := capture.Call(context.Background(), captureSocket(p), capture.Request{Action: "start", Start: &capture.StartOptions{Profile: "work", Backend: backend, Owner: "cli"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = rewriteActivate(context.Background(), p, "work", snapshot); e != nil {
		t.Fatal(e)
	}
	if len(*children) != 1 {
		t.Fatal("missing capture child")
	}
	worker := (*children)[0]
	if worker.configured.Capture == nil || worker.configured.Capture.SessionID != r.Status.SessionID || worker.configured.RecorderSocket != captureSocket(p) {
		t.Fatal(worker.configured)
	}
	domains := engine.candidate["intercept_domains"].([]any)
	if len(domains) != 0 || engine.candidate["rule_version"] != snapshot.Version() {
		t.Fatal(engine.candidate)
	}
	saved, e := (rewrite.Store{ConfigDir: p.ConfigDir, Profile: "work"}).Load()
	if e != nil || saved.Document().Enabled || saved.Version() != snapshot.Version() {
		t.Fatal("capture changed global intent", e)
	}
	effective := saved.Effective()
	if effective.HasEnabledRules() || effective.Version() != saved.Version() {
		t.Fatal("disabled rules applied during capture")
	}
}
func TestDraftCLISaveVersionConflictIsAtomic(t *testing.T) {
	p := isolatedTestPaths(t, t.TempDir())
	base := rewrite.Empty()
	ruleSnapshot, e := rewrite.Parse([]byte(`{"version":2,"enabled":false,"rules":[` + rewriteTestRule + `],"scripts":[]}`))
	if e != nil {
		t.Fatal(e)
	}
	rule := ruleSnapshot.Document().Rules[0]
	draft := map[string]any{"base_rule_version": base.Version(), "position": 0, "rule": rule, "enable": false}
	b, _ := json.Marshal(draft)
	v := rewriteAliasOK(t, "rewrite", string(b), "draft", "save", "-")
	if v["saved"] != true {
		t.Fatal(v)
	}
	before, e := os.ReadFile(rewrite.Path(p.ConfigDir, "default"))
	if e != nil {
		t.Fatal(e)
	}
	code, _, err := rewriteCLIRun(t, string(b), "--json", "request-rewrite", "draft", "save", "-")
	if code != 1 || !strings.Contains(err, "rewrite_conflict") {
		t.Fatal(code, err)
	}
	after, _ := os.ReadFile(rewrite.Path(p.ConfigDir, "default"))
	if !bytes.Equal(before, after) {
		t.Fatal("conflict changed rules")
	}
}

func TestCaptureStartPauseResumeStopKeepGenerationAndHistory(t *testing.T) {
	p, engine, children := rewriteCoordinatorFixture(t)
	startTestCaptureRecorder(t, p)
	priorCapture := rewriteCaptureControl
	t.Cleanup(func() { rewriteCaptureControl = priorCapture })
	var policies []capture.Policy
	rewriteCaptureControl = func(ctx context.Context, socket, generation string, policy capture.Policy) (child.Status, error) {
		policies = append(policies, policy)
		(*children)[len(*children)-1].configured.Capture = &policy
		return child.Status{Generation: generation, Ready: true}, nil
	}
	rewriteClient = func(nagiruntime.Paths) (*control.Client, error) {
		return control.NewWithBridge(func(ctx context.Context, method, path string, body, out any) error {
			if path == "/request-rewrite/capabilities" {
				b, _ := json.Marshal(control.RewriteCapabilities{Versions: []int{2}, CaptureProtocol: 1, TUNOnly: true, OriginalContext: true, Reinjection: true, QUIC: true, MaxContexts: 1024, MaxStreams: 64, ContextIdle: 60, ContextMax: 3600})
				return json.Unmarshal(b, out)
			}
			if path == "/request-rewrite/capture" {
				engine.calls = append(engine.calls, "PUT "+path)
				policy := body.(control.RewriteCapture).Capture
				engine.active.Capture = &policy
				return nil
			}
			return engine.call(ctx, method, path, body, out)
		}, nil), nil
	}
	v := rewriteAliasOK(t, "rewrite", `{"owner":"cli","scope":{"domains":[],"process_paths":[],"app_paths":[]}}`, "capture", "start", "-")
	s := v["status"].(map[string]any)
	session := s["session_id"].(string)
	generation := engine.active.Generation
	record := capture.Record{ID: "retained", SessionID: session, Kind: "http", Identity: capture.Identity{SessionID: session, Revision: 1}, Outcome: "complete", Snapshots: map[string]capture.Snapshot{}}
	_, e := capture.Call(context.Background(), captureSocket(p), capture.Request{Action: "event", SessionID: session, Event: &capture.Event{Type: "record", SessionID: session, RecordID: record.ID, Record: &record}})
	if e != nil {
		t.Fatal(e)
	}
	for _, action := range []string{"pause", "resume", "stop"} {
		rewriteAliasOK(t, "request-rewrite", "", "capture", action, session)
		if engine.active.Generation != generation || len(*children) != 1 {
			t.Fatal("policy update replaced rule generation")
		}
	}
	r, e := capture.Call(context.Background(), captureSocket(p), capture.Request{Action: "show", RecordID: record.ID})
	if e != nil || r.Record == nil {
		t.Fatal("history lost", e)
	}
	if len(policies) != 4 || policies[3].State != "stopped" || policies[3].Revision != 4 {
		t.Fatal(policies)
	}
}

func captureCoordinatorTest(t *testing.T) (nagiruntime.Paths, *rewriteFixtureEngine, *[]*rewriteFixtureChild) {
	p, engine, children := rewriteCoordinatorFixture(t)
	startTestCaptureRecorder(t, p)
	old := rewriteCaptureControl
	t.Cleanup(func() { rewriteCaptureControl = old })
	rewriteCaptureControl = func(_ context.Context, _ string, generation string, policy capture.Policy) (child.Status, error) {
		(*children)[len(*children)-1].configured.Capture = &policy
		return child.Status{Generation: generation, Ready: true, Capture: &policy}, nil
	}
	rewriteClient = func(nagiruntime.Paths) (*control.Client, error) {
		return control.NewWithBridge(func(ctx context.Context, method, path string, body, out any) error {
			if path == "/request-rewrite/capabilities" {
				b, _ := json.Marshal(control.RewriteCapabilities{Versions: []int{2}, CaptureProtocol: 1, TUNOnly: true, OriginalContext: true, Reinjection: true, QUIC: true, MaxContexts: 1024, MaxStreams: 64, ContextIdle: 60, ContextMax: 3600})
				return json.Unmarshal(b, out)
			}
			if path == "/request-rewrite/capture" {
				engine.calls = append(engine.calls, "PUT "+path)
				policy := body.(control.RewriteCapture).Capture
				engine.active.Capture = &policy
				b, _ := json.Marshal(engine.active)
				return json.Unmarshal(b, out)
			}
			return engine.call(ctx, method, path, body, out)
		}, nil), nil
	}
	return p, engine, children
}

func TestCaptureStatusRequiresAllRuntimeEvidence(t *testing.T) {
	p, engine, children := captureCoordinatorTest(t)
	v := rewriteAliasOK(t, "rewrite", "", "capture", "start")
	session := v["status"].(map[string]any)["session_id"].(string)
	original := engine.active
	for _, change := range []func(){func() { engine.active.TUNRunning = false }, func() { engine.active.BranchEnabled = false }, func() { engine.active.Capture = nil }, func() { engine.active.EngineEpoch = "changed" }, func() { engine.active.State = "blocked" }, func() { (*children)[0].configured.Capture = nil }} {
		engine.active = original
		(*children)[0].configured.Capture = original.Capture
		change()
		r := rewriteAliasOK(t, "rewrite", "", "capture", "status")["status"].(map[string]any)
		if r["state"] != "uncertain" || r["recorder_state"] != "recording" {
			t.Fatal(r)
		}
		code, _, err := rewriteCLIRun(t, "", "--json", "rewrite", "capture", "pause", session)
		if code != 1 || !strings.Contains(err, "capture_state_uncertain") {
			t.Fatal(code, err)
		}
	}
	engine.active = original
	(*children)[0].configured.Capture = original.Capture
	if captureObserved(context.Background(), p, captureStatus(context.Background(), p)).State != "recording" {
		t.Fatal("coherent runtime not recording")
	}
}

func TestCapturePartialPublishHasExplicitStopRecovery(t *testing.T) {
	p, engine, children := captureCoordinatorTest(t)
	v := rewriteAliasOK(t, "rewrite", "", "capture", "start")
	session := v["status"].(map[string]any)["session_id"].(string)
	rewriteCaptureControl = func(context.Context, string, string, capture.Policy) (child.Status, error) {
		return child.Status{}, errors.New("fixture failure")
	}
	code, _, err := rewriteCLIRun(t, "", "--json", "rewrite", "capture", "pause", session)
	if code != 1 || !strings.Contains(err, "capture_state_uncertain") {
		t.Fatal(code, err)
	}
	if engine.active.Capture.State != "paused" || (*children)[0].configured.Capture.State != "recording" {
		t.Fatal("fixture did not diverge")
	}
	if captureObserved(context.Background(), p, captureStatus(context.Background(), p)).State != "uncertain" {
		t.Fatal("false status")
	}
	stopped := rewriteAliasOK(t, "rewrite", "", "capture", "stop", session)["status"].(map[string]any)
	if stopped["state"] != "stopped" || engine.active.BranchEnabled {
		t.Fatal(stopped, engine.active)
	}
	if _, e := readRewriteRun(p); !os.IsNotExist(e) {
		t.Fatal("runtime not retired", e)
	}
}

func TestCaptureOptionsRejectDuplicateKeysBeforeMutation(t *testing.T) {
	p := isolatedTestPaths(t, t.TempDir())
	startTestCaptureRecorder(t, p)
	for _, input := range []string{`{"query":"one","query":"two"}`, `{"limit":10,"limit":20}`, `{"Limit":10}`, `null`} {
		code, _, err := rewriteCLIRun(t, input, "--json", "rewrite", "capture", "list", "-")
		if code != 1 || !strings.Contains(err, "capture_options_invalid") {
			t.Fatal(code, err)
		}
	}
}

func TestCaptureLostPolicyAcknowledgmentsUseReadEvidence(t *testing.T) {
	p, _, _ := captureCoordinatorTest(t)
	v := rewriteAliasOK(t, "rewrite", "", "capture", "start")
	session := v["status"].(map[string]any)["session_id"].(string)
	originalClient := rewriteClient
	puts := 0
	rewriteClient = func(p nagiruntime.Paths) (*control.Client, error) {
		base, e := originalClient(p)
		if e != nil {
			return nil, e
		}
		return control.NewWithBridge(func(ctx context.Context, method, path string, body, out any) error {
			if method == "PUT" {
				err := base.Put(ctx, path, body, out)
				if path == "/request-rewrite/capture" {
					puts++
					return errors.New("lost engine acknowledgment")
				}
				return err
			}
			return base.Get(ctx, path, out)
		}, nil), nil
	}
	originalControl := rewriteCaptureControl
	calls := 0
	rewriteCaptureControl = func(ctx context.Context, socket, generation string, policy capture.Policy) (child.Status, error) {
		_, e := originalControl(ctx, socket, generation, policy)
		if e != nil {
			return child.Status{}, e
		}
		calls++
		return child.Status{}, errors.New("lost child acknowledgment")
	}
	rewriteAliasOK(t, "rewrite", "", "capture", "pause", session)
	if puts != 1 || calls != 1 {
		t.Fatal("policy mutation retried", puts, calls)
	}
	if captureObserved(context.Background(), p, captureStatus(context.Background(), p)).State != "paused" {
		t.Fatal("lost successful acknowledgments not reconciled")
	}
}
