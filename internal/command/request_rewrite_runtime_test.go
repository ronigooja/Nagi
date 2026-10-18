package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/profile"
	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
	child "github.com/ronigooja/Nagi/internal/requestrewriteservice"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

type rewriteFixtureEngine struct {
	active                                          rewriteEngineStatus
	candidate                                       map[string]any
	calls                                           []string
	failProbe, failPublish, lostPublish, failStatus bool
}

func (f *rewriteFixtureEngine) call(ctx context.Context, method, path string, body, out any) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	f.calls = append(f.calls, method+" "+strings.Split(path, "?")[0])
	var response any
	switch {
	case path == "/request-rewrite/capabilities":
		response = map[string]any{"versions": []int{1}, "tun_only": true, "original_context": true, "reinjection": true, "quic_domain_block": true}
	case path == "/request-rewrite/status":
		if f.failStatus {
			return errors.New("fixture credential must not be echoed")
		}
		response = f.active
	case method == "PUT" && path == "/request-rewrite/session":
		b, _ := json.Marshal(body)
		_ = json.Unmarshal(b, &f.candidate)
		if f.candidate["owner_uid"] != float64(1000) {
			return fmt.Errorf("owner not bound")
		}
		if f.candidate["handoff_secret"] == f.candidate["reinjection_secret"] {
			return fmt.Errorf("duplicate secrets")
		}
		response = map[string]any{"engine_epoch": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", "reinjection_addr": "127.0.0.1:1", "engine_transport_cert": "PUBLIC_FIXTURE"}
	case path == "/request-rewrite/probe":
		if f.failProbe {
			return errors.New("fixture probe fail")
		}
		response = map[string]bool{"ready": true}
	case method == "PUT" && path == "/request-rewrite/branch":
		b, _ := json.Marshal(body)
		var request struct {
			Generation string `json:"generation"`
			Enabled    bool   `json:"enabled"`
		}
		_ = json.Unmarshal(b, &request)
		if request.Enabled {
			if f.failPublish {
				return &control.APIError{Status: 409, Message: "fixture reject"}
			}
			f.active = rewriteEngineStatus{Version: 1, Generation: request.Generation, EngineEpoch: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", State: "active", BranchEnabled: true, TUNRunning: true, RuleVersion: f.candidate["rule_version"].(string)}
		} else {
			f.active.State = "draining"
			f.active.BranchEnabled = false
		}
		if f.lostPublish && request.Enabled {
			return errors.New("lost acknowledgement")
		}
		response = f.active
	case method == "DELETE":
		if strings.Contains(path, f.active.Generation) && f.active.Generation != "" {
			if f.active.BranchEnabled {
				return errors.New("active delete")
			}
			f.active = rewriteEngineStatus{Version: 1, State: "absent", TUNRunning: true}
		}
		f.candidate = nil
		response = map[string]bool{"closed": true}
	default:
		return fmt.Errorf("unexpected API %s %s", method, path)
	}
	if out != nil {
		b, _ := json.Marshal(response)
		return json.Unmarshal(b, out)
	}
	return nil
}

type rewriteFixtureChild struct {
	closed     bool
	configured child.Configuration
	events     *[]string
}

func (c *rewriteFixtureChild) Announcement() child.Announcement {
	return child.Announcement{HandoffAddr: "127.0.0.1:2", ChildTransportCert: "FIXTURE_PUBLIC_CERT"}
}
func (c *rewriteFixtureChild) Configure(v child.Configuration) error {
	c.configured = v
	*c.events = append(*c.events, "configure")
	return nil
}
func (c *rewriteFixtureChild) Close() error { c.closed = true; return nil }
func (c *rewriteFixtureChild) PID() int     { return 12345 }
func rewriteCoordinatorFixture(t *testing.T) (nagiruntime.Paths, *rewriteFixtureEngine, *[]*rewriteFixtureChild) {
	t.Helper()
	paths := isolatedTestPaths(t, t.TempDir())
	if e := paths.EnsureRuntime(); e != nil {
		t.Fatal(e)
	}
	if e := profile.NewStore(paths.ConfigDir, func([]byte) error { return nil }, nil).Apply(context.Background(), "work", []byte("mode: rule\n")); e != nil {
		t.Fatal(e)
	}
	if _, e := (rewrite.CAStore{ConfigDir: paths.ConfigDir}).Generate(time.Now()); e != nil {
		t.Fatal(e)
	}
	previousIdentity := rewriteProcessIdentity
	previousEngineIdentity := rewriteEngineIdentity
	previousUID, previousStart, previousControl, previousSupervisor, previousClient, previousTrust := rewriteEffectiveUID, rewriteStartChild, rewriteChildControl, rewriteStartSupervisor, rewriteClient, rewriteTrustAssessment
	t.Cleanup(func() {
		rewriteProcessIdentity = previousIdentity
		rewriteEngineIdentity = previousEngineIdentity
		rewriteEffectiveUID = previousUID
		rewriteStartChild = previousStart
		rewriteChildControl = previousControl
		rewriteStartSupervisor = previousSupervisor
		rewriteClient = previousClient
		rewriteTrustAssessment = previousTrust
	})
	rewriteEffectiveUID = func() int { return 1000 }
	rewriteProcessIdentity = func(int) (string, error) { return "fixture-start-identity", nil }
	rewriteEngineIdentity = func(context.Context, nagiruntime.Paths) (int, string, error) {
		return 12346, "fixture-start-identity", nil
	}
	rewriteTrustAssessment = func([]byte) string { return "trusted" }
	engine := &rewriteFixtureEngine{active: rewriteEngineStatus{Version: 1, State: "absent", TUNRunning: true}}
	rewriteClient = func(nagiruntime.Paths) (*control.Client, error) { return control.NewWithBridge(engine.call, nil), nil }
	children := []*rewriteFixtureChild{}
	rewriteStartChild = func(_ context.Context, _ string, opt child.ChildOptions) (managedRewriteChild, error) {
		if _, e := rewrite.Parse(opt.Document); e != nil {
			return nil, e
		}
		c := &rewriteFixtureChild{events: &engine.calls}
		children = append(children, c)
		engine.calls = append(engine.calls, "start")
		return c, nil
	}
	rewriteStartSupervisor = func(_ nagiruntime.Paths, generation string) error {
		engine.calls = append(engine.calls, "supervise")
		return nil
	}
	rewriteChildControl = func(_ context.Context, _ string, method, path, generation string) (child.Status, error) {
		engine.calls = append(engine.calls, method+" child"+path)
		if path == "/shutdown" {
			return child.Status{}, nil
		}
		return child.Status{Version: 1, Generation: generation, RuleVersion: engine.active.RuleVersion, Ready: true}, nil
	}
	return paths, engine, &children
}
func rewriteEnabledFixture(t *testing.T) *rewrite.Snapshot {
	t.Helper()
	d := rewrite.Empty().Document()
	d.Enabled = true
	d.Domains = []rewrite.Domain{{ID: "api", Pattern: "api.service.example"}}
	s, e := rewrite.Compile(d)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestRewriteCoordinatorStagesPublishesAndDisables(t *testing.T) {
	paths, engine, children := rewriteCoordinatorFixture(t)
	snapshot := rewriteEnabledFixture(t)
	if e := rewriteActivate(context.Background(), paths, "work", snapshot); e != nil {
		t.Fatal(e)
	}
	if len(*children) != 1 || (*children)[0].closed || !engine.active.BranchEnabled {
		t.Fatal("missing live generation")
	}
	calls := strings.Join(engine.calls, "|")
	for _, sequence := range []string{"start|supervise|PUT /request-rewrite/session|configure|POST /request-rewrite/probe|PUT /request-rewrite/branch"} {
		if !strings.Contains(calls, sequence) {
			t.Fatalf("staging order %s", calls)
		}
	}
	run, e := readRewriteRun(paths)
	if e != nil || run.RuleVersion != snapshot.Version() || run.Previous != nil {
		t.Fatalf("journal %v %v", run, e)
	}
	encoded, _ := os.ReadFile(filepath.Join(paths.RuntimeDir, "request-rewrite-run.json"))
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "PUBLIC") {
		t.Fatal("sensitive journal")
	}
	engine.calls = nil
	if e = rewriteTeardown(context.Background(), paths); e != nil {
		t.Fatal(e)
	}
	calls = strings.Join(engine.calls, "|")
	if !strings.Contains(calls, "PUT /request-rewrite/branch|POST child/drain|DELETE /request-rewrite/session|GET /request-rewrite/status|POST child/shutdown") {
		t.Fatalf("disable order %s", calls)
	}
	if _, e = readRewriteRun(paths); !os.IsNotExist(e) {
		t.Fatal("journal retained after confirmed cleanup")
	}
}
func TestRewriteCoordinatorProbeFailureNeverPublishes(t *testing.T) {
	paths, engine, children := rewriteCoordinatorFixture(t)
	engine.failProbe = true
	if e := rewriteActivate(context.Background(), paths, "work", rewriteEnabledFixture(t)); e == nil {
		t.Fatal("probe accepted")
	}
	if engine.active.BranchEnabled || len(*children) != 1 || !(*children)[0].closed {
		t.Fatal("failure leaked branch or child")
	}
	for _, call := range engine.calls {
		if call == "PUT /request-rewrite/branch" {
			t.Fatal("published after failed probe")
		}
	}
}
func TestRewriteCoordinatorLostPublishChecksEvidence(t *testing.T) {
	paths, engine, _ := rewriteCoordinatorFixture(t)
	engine.lostPublish = true
	if e := rewriteActivate(context.Background(), paths, "work", rewriteEnabledFixture(t)); e != nil {
		t.Fatal(e)
	}
	if !engine.active.BranchEnabled {
		t.Fatal("did not observe late success")
	}
}
func TestRewriteCoordinatorStatusNeverInfersRunning(t *testing.T) {
	paths, engine, _ := rewriteCoordinatorFixture(t)
	engine.failStatus = true
	state, e := requestRewriteStatus(context.Background(), paths)
	if e != nil {
		t.Fatal(e)
	}
	if state.Actual != "unavailable" || state.TUNRunning != nil || state.ServiceRunning != nil || state.ProtocolVersion != nil {
		t.Fatalf("unsupported claim %#v", state)
	}
	data, _ := json.Marshal(state)
	if strings.Contains(string(data), "credential") {
		t.Fatal("raw failure leaked")
	}
}
func TestRewriteRuntimeStateRejectsSymlinkAndPublicFile(t *testing.T) {
	paths, _, _ := rewriteCoordinatorFixture(t)
	file := filepath.Join(paths.RuntimeDir, "request-rewrite-run.json")
	target := filepath.Join(t.TempDir(), "target")
	if e := os.WriteFile(target, []byte("private"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(target, file); e != nil {
		t.Fatal(e)
	}
	if _, e := readRewriteRun(paths); e == nil {
		t.Fatal("followed symlink")
	}
	os.Remove(file)
	if e := os.WriteFile(file, []byte(`{}`), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := readRewriteRun(paths); e == nil {
		t.Fatal("accepted public journal")
	}
}
func TestRewriteDisableRecoversInvalidSelection(t *testing.T) {
	paths, engine, _ := rewriteCoordinatorFixture(t)
	engine.active.State = "absent"
	if e := os.RemoveAll(filepath.Join(paths.ConfigDir, "profiles")); e != nil {
		t.Fatal(e)
	}
	_, e := requestRewriteSetEnabled(context.Background(), paths, false)
	// Missing selection may still resolve the default name. It must reach the
	// runtime teardown path regardless of unrelated profile contents.
	if e != nil && !strings.Contains(e.Error(), "rewrite_invalid") {
		t.Fatal(e)
	}
	if engine.active.BranchEnabled {
		t.Fatal("disable left branch")
	}
}

func TestRewriteLiveMutationFailureRestoresDiskAndPreviousGeneration(t *testing.T) {
	paths, engine, children := rewriteCoordinatorFixture(t)
	store := rewriteStore(paths, "work")
	if _, e := store.Save(context.Background(), rewriteEnabledFixture(t)); e != nil {
		t.Fatal(e)
	}
	previous, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	generation := engine.active.Generation
	engine.failProbe = true
	_, err = store.AddDomain(context.Background(), rewrite.Domain{ID: "mirror", Pattern: "mirror.service.example"})
	if err == nil {
		t.Fatal("failed candidate accepted")
	}
	restored, e := store.Load()
	if e != nil || restored.Version() != previous.Version() {
		t.Fatalf("disk not restored %v", e)
	}
	if engine.active.Generation != generation || !engine.active.BranchEnabled || engine.active.RuleVersion != previous.Version() {
		t.Fatalf("runtime not restored %#v", engine.active)
	}
	if len(*children) != 2 || (*children)[0].closed || !(*children)[1].closed {
		t.Fatal("incorrect child ownership after rejected mutation")
	}
	run, e := readRewriteRun(paths)
	if e != nil || run.Generation != generation || run.Previous != nil {
		t.Fatalf("prior journal not restored %#v %v", run, e)
	}
}
func TestRewriteUncertainPublishKeepsChildAndRecoveryOwnership(t *testing.T) {
	paths, engine, children := rewriteCoordinatorFixture(t)
	// Lose both the acknowledgement and the recovery observation only after
	// publication, leaving final state deliberately unknowable to the caller.
	previousClient := rewriteClient
	rewriteClient = func(paths nagiruntime.Paths) (*control.Client, error) {
		return control.NewWithBridge(func(ctx context.Context, method, path string, body, out any) error {
			e := engine.call(ctx, method, path, body, out)
			if path == "/request-rewrite/branch" && method == "PUT" {
				engine.failStatus = true
				return errors.New("lost ack")
			}
			return e
		}, nil), nil
	}
	defer func() { rewriteClient = previousClient }()
	e := rewriteActivate(context.Background(), paths, "work", rewriteEnabledFixture(t))
	if e == nil || !strings.Contains(e.Error(), "rewrite_state_uncertain") {
		t.Fatalf("uncertain publish reported %v", e)
	}
	if len(*children) != 1 || (*children)[0].closed {
		t.Fatal("closed possibly active child")
	}
	if _, e = readRewriteRun(paths); e != nil {
		t.Fatal("lost candidate ownership")
	}
}
func TestRewriteDisableFailurePreservesServiceAndIntent(t *testing.T) {
	paths, engine, children := rewriteCoordinatorFixture(t)
	store := rewriteStore(paths, "work")
	if _, e := store.Save(context.Background(), rewriteEnabledFixture(t)); e != nil {
		t.Fatal(e)
	}
	engine.failStatus = true
	if e := os.WriteFile(paths.SocketPath, []byte("controller fixture present"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := store.Edit(context.Background(), func(d *rewrite.Document) error { d.Enabled = false; return nil }); e == nil {
		t.Fatal("disable claimed success without status")
	}
	persisted, e := store.Load()
	if e != nil || !persisted.Document().Enabled {
		t.Fatal("lost enabled intent on uncertain disable")
	}
	if len(*children) != 1 || (*children)[0].closed {
		t.Fatal("closed old service without removing branch")
	}
	if _, e = readRewriteRun(paths); e != nil {
		t.Fatal("lost recovery journal")
	}
}
func TestRewriteDisabledMutationDoesNotStartChild(t *testing.T) {
	paths, engine, children := rewriteCoordinatorFixture(t)
	store := rewriteStore(paths, "work")
	if _, e := store.AddDomain(context.Background(), rewrite.Domain{ID: "api", Pattern: "api.service.example"}); e != nil {
		t.Fatal(e)
	}
	if len(*children) != 0 || engine.active.BranchEnabled {
		t.Fatal("disabled edit activated rewrite")
	}
}
func TestRewriteLifecycleIntentSurvivesTeardown(t *testing.T) {
	paths, _, _ := rewriteCoordinatorFixture(t)
	store := rewriteStore(paths, "work")
	if _, e := store.Save(context.Background(), rewriteEnabledFixture(t)); e != nil {
		t.Fatal(e)
	}
	if e := rewriteLifecycleStop(context.Background(), paths); e != nil {
		t.Fatal(e)
	}
	saved, e := store.Load()
	if e != nil || !saved.Document().Enabled {
		t.Fatal("lifecycle stop cleared explicit rewrite intent")
	}
}
