package command

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/profile"
	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
	child "github.com/ronigooja/Nagi/internal/requestrewriteservice"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

// RewriteStatus reports evidence separately from persisted intent. No endpoint,
// transport credential or application content is included in this interface.
type RewriteStatus struct {
	Profile               string           `json:"profile"`
	EnabledIntent         bool             `json:"enabled_intent"`
	Actual                string           `json:"actual"`
	ConfiguredRuleVersion string           `json:"configured_rule_version"`
	ActiveRuleVersion     string           `json:"active_rule_version,omitempty"`
	Generation            string           `json:"generation,omitempty"`
	EngineEpoch           string           `json:"engine_epoch,omitempty"`
	TUNRunning            *bool            `json:"tun_running"`
	Backend               string           `json:"backend"`
	ServiceRunning        *bool            `json:"service_running"`
	ProtocolVersion       *int             `json:"protocol_version"`
	CA                    rewrite.CAStatus `json:"ca"`
	LastError             *string          `json:"last_error"`
	Recovery              string           `json:"recovery,omitempty"`
}

type rewriteEngineStatus struct {
	Version            int     `json:"version"`
	Generation         string  `json:"generation"`
	EngineEpoch        string  `json:"engine_epoch"`
	State              string  `json:"state"`
	BranchEnabled      bool    `json:"branch_enabled"`
	TUNRunning         bool    `json:"tun_running"`
	RuleVersion        string  `json:"rule_version"`
	PreparedGeneration string  `json:"prepared_generation"`
	Contexts           int     `json:"contexts"`
	LastError          *string `json:"last_error"`
}

type rewriteRun struct {
	Version     int         `json:"version"`
	Profile     string      `json:"profile"`
	Backend     string      `json:"backend"`
	Generation  string      `json:"generation"`
	EngineEpoch string      `json:"engine_epoch"`
	RuleVersion string      `json:"rule_version"`
	PID         int         `json:"pid"`
	RenewAt     time.Time   `json:"renew_at"`
	CreatedAt   time.Time   `json:"created_at"`
	Previous    *rewriteRun `json:"previous,omitempty"`
}

func rewriteFailure(code string) error {
	return fail("request_rewrite_error", fmt.Errorf("%s; run `nagi request-rewrite status` or `nagi request-rewrite disable`", code))
}
func rewriteDomainError(err error) error {
	if err == nil {
		return nil
	}
	var e *rewrite.Error
	if errors.As(err, &e) {
		return rewriteFailure(e.Code)
	}
	return rewriteFailure("rewrite_state_uncertain")
}

// The caller holds config.lock before Store takes its document lock. Activation
// callbacks deliberately never reacquire config.lock.
func rewriteStore(paths nagiruntime.Paths, name string) rewrite.Store {
	return rewrite.Store{ConfigDir: paths.ConfigDir, Profile: name, Activation: rewriteActivation{paths: paths, profile: name}}
}
func rewriteSelected(paths nagiruntime.Paths) (string, error) {
	return profile.NewStore(paths.ConfigDir, nil, nil).Current()
}

func requestRewriteStatus(ctx context.Context, paths nagiruntime.Paths) (RewriteStatus, error) {
	name, err := rewriteSelected(paths)
	if err != nil {
		return RewriteStatus{Actual: "unavailable", Recovery: "nagi profile list"}, rewriteFailure("rewrite_invalid")
	}
	return rewriteStatusFor(ctx, paths, name)
}
func rewriteStatusFor(ctx context.Context, paths nagiruntime.Paths, name string) (RewriteStatus, error) {
	out := RewriteStatus{Profile: name, Actual: "unavailable", Recovery: "nagi request-rewrite disable"}
	snapshot, err := (rewrite.Store{ConfigDir: paths.ConfigDir, Profile: name}).Load()
	if err != nil {
		code := "rewrite_invalid"
		out.LastError = &code
		return out, rewriteDomainError(err)
	}
	out.EnabledIntent = snapshot.Document().Enabled
	out.ConfiguredRuleVersion = snapshot.Version()
	out.CA, _ = requestRewriteCAStatus(paths)
	out.Backend, _ = readBackend(paths)
	client, err := controlClientForCurrentBackend(paths)
	if err != nil {
		code := "rewrite_unavailable"
		out.LastError = &code
		return out, nil
	}
	var state rewriteEngineStatus
	if err = client.Get(ctx, "/request-rewrite/status", &state); err != nil {
		code := "rewrite_unavailable"
		var api *control.APIError
		if errors.As(err, &api) && api.Status == 404 {
			code = "rewrite_capability_missing"
			out.Recovery = "nagi update"
		}
		out.LastError = &code
		return out, nil
	}
	out.TUNRunning = &state.TUNRunning
	out.ProtocolVersion = &state.Version
	out.Generation = state.Generation
	out.EngineEpoch = state.EngineEpoch
	out.ActiveRuleVersion = state.RuleVersion
	out.Actual = state.State
	out.LastError = state.LastError
	if out.Actual == "prepared" {
		out.Actual = "starting"
	}
	if state.State == "active" {
		run, e := readRewriteRun(paths)
		if run.Previous != nil && run.Previous.Generation == state.Generation {
			run = *run.Previous
		}
		if e != nil || run.Generation != state.Generation || run.Profile != name || run.RuleVersion != state.RuleVersion {
			out.Actual = "uncertain"
			code := "rewrite_state_uncertain"
			out.LastError = &code
			return out, nil
		}
		health, e := child.Control(ctx, rewriteSocket(paths, state.Generation), "GET", "/status", state.Generation)
		running := e == nil
		out.ServiceRunning = &running
		if e != nil || !health.Ready || health.Generation != state.Generation || health.RuleVersion != state.RuleVersion {
			out.Actual = "blocked"
			code := "rewrite_unavailable"
			out.LastError = &code
		}
	}
	if out.Actual == "absent" {
		out.Actual = "disabled"
	}
	if out.Actual == "active" && out.EnabledIntent && out.ActiveRuleVersion == out.ConfiguredRuleVersion {
		out.Recovery = ""
	}
	return out, nil
}

func requestRewriteSetEnabled(ctx context.Context, paths nagiruntime.Paths, enabled bool) (RewriteStatus, error) {
	name, e := rewriteSelected(paths)
	if e != nil {
		return RewriteStatus{}, rewriteFailure("rewrite_invalid")
	}
	// Disable remains a recovery operation even if the document cannot be parsed.
	store := rewriteStore(paths, name)
	if _, e = store.Load(); e != nil && !enabled {
		if e = rewriteTeardown(ctx, paths); e != nil {
			return RewriteStatus{}, e
		}
		return RewriteStatus{Profile: name, Actual: "disabled", Recovery: "nagi request-rewrite import FILE"}, rewriteFailure("rewrite_invalid")
	}
	_, e = store.Edit(ctx, func(d *rewrite.Document) error { d.Enabled = enabled; return nil })
	if e != nil {
		return RewriteStatus{}, rewriteDomainError(e)
	}
	return rewriteStatusFor(ctx, paths, name)
}

type rewriteActivation struct {
	paths   nagiruntime.Paths
	profile string
}

func (a rewriteActivation) Apply(ctx context.Context, old, candidate *rewrite.Snapshot) error {
	selected, e := rewriteSelected(a.paths)
	if e != nil {
		return e
	}
	if selected != a.profile {
		return nil
	}
	if !candidate.Document().Enabled {
		return rewriteTeardown(ctx, a.paths)
	}
	return rewriteActivate(ctx, a.paths, a.profile, candidate)
}
func (a rewriteActivation) Rollback(ctx context.Context, old *rewrite.Snapshot) error {
	selected, e := rewriteSelected(a.paths)
	if e != nil {
		return e
	}
	if selected != a.profile {
		return nil
	}
	if !old.Document().Enabled {
		return rewriteTeardown(ctx, a.paths)
	}
	return rewriteActivate(ctx, a.paths, a.profile, old)
}

// All lifecycle callers hold config.lock. This path intentionally does not
// enable TUN, start mihomo, generate a CA or install OS trust implicitly.
func rewriteReconcile(ctx context.Context, paths nagiruntime.Paths) error {
	name, e := rewriteSelected(paths)
	if e != nil {
		return nil
	}
	snapshot, e := (rewrite.Store{ConfigDir: paths.ConfigDir, Profile: name}).Load()
	if e != nil {
		return rewriteDomainError(e)
	}
	if !snapshot.Document().Enabled {
		return rewriteTeardown(ctx, paths)
	}
	return rewriteActivate(ctx, paths, name, snapshot)
}

var rewriteStartChild = child.StartChild
var rewriteNow = time.Now
var rewriteStartSupervisor = startRewriteSupervisor

func rewriteActivate(ctx context.Context, paths nagiruntime.Paths, name string, snapshot *rewrite.Snapshot) error {
	if os.Geteuid() == 0 {
		return rewriteFailure("rewrite_auth")
	}
	if len(snapshot.Document().Domains) == 0 {
		return rewriteFailure("rewrite_invalid")
	}
	client, e := controlClientForCurrentBackend(paths)
	if e != nil {
		return rewriteFailure("rewrite_unavailable")
	}
	var capabilities struct {
		Versions        []int `json:"versions"`
		TUNOnly         bool  `json:"tun_only"`
		OriginalContext bool  `json:"original_context"`
		Reinjection     bool  `json:"reinjection"`
		QUIC            bool  `json:"quic_domain_block"`
	}
	if e = client.Get(ctx, "/request-rewrite/capabilities", &capabilities); e != nil {
		return rewriteFailure("rewrite_capability_missing")
	}
	supported := false
	for _, v := range capabilities.Versions {
		supported = supported || v == 1
	}
	if !supported || !capabilities.TUNOnly || !capabilities.OriginalContext || !capabilities.Reinjection || !capabilities.QUIC {
		return rewriteFailure("rewrite_capability_missing")
	}
	var current rewriteEngineStatus
	if client.Get(ctx, "/request-rewrite/status", &current) != nil {
		return rewriteFailure("rewrite_unavailable")
	}
	if !current.TUNRunning {
		return rewriteFailure("rewrite_tun_required")
	}
	ca, e := requestRewriteCAStatus(paths)
	if e != nil || ca.State != "generated" {
		return rewriteFailure("rewrite_ca_required")
	}
	if ca.Trust != "trusted" {
		return rewriteFailure("rewrite_ca_trust_required")
	}

	if run, e := readRewriteRun(paths); e == nil && current.State == "active" && run.Profile == name && current.RuleVersion == snapshot.Version() && run.Generation == current.Generation && rewriteNow().Before(run.RenewAt) {
		health, e := child.Control(ctx, rewriteSocket(paths, run.Generation), "GET", "/status", run.Generation)
		if e == nil && health.Ready && health.RuleVersion == snapshot.Version() {
			return nil
		}
	}
	if r, e := readRewriteRun(paths); e == nil && r.Previous != nil {
		return rewriteFailure("rewrite_state_uncertain")
	}
	if current.PreparedGeneration != "" || current.State == "prepared" {
		return rewriteFailure("rewrite_state_uncertain")
	}
	generation, e := rewriteRandom(16, false)
	if e != nil {
		return e
	}
	handoff, e := rewriteRandom(32, true)
	if e != nil {
		return e
	}
	reinjection, e := rewriteRandom(32, true)
	if e != nil {
		return e
	}
	doc, e := json.Marshal(snapshot.Document())
	if e != nil {
		return rewriteFailure("rewrite_invalid")
	}
	executable, e := os.Executable()
	if e != nil {
		return rewriteFailure("rewrite_unavailable")
	}
	if e = paths.EnsureRuntime(); e != nil {
		return rewriteFailure("rewrite_storage")
	}
	worker, e := rewriteStartChild(ctx, executable, child.ChildOptions{CADir: paths.ConfigDir, RuntimeDir: paths.RuntimeDir, Generation: generation, Document: doc})
	if e != nil {
		return rewriteFailure("rewrite_activation_failed")
	}
	published := false
	defer func() {
		if !published {
			worker.Close()
		}
	}()
	var previous *string
	if current.BranchEnabled {
		v := current.Generation
		previous = &v
	}
	request := map[string]any{"version": 1, "generation": generation, "previous_generation": previous, "owner_uid": os.Geteuid(), "handoff_addr": worker.Announcement.HandoffAddr, "child_transport_cert": worker.Announcement.ChildTransportCert, "handoff_secret": handoff, "reinjection_secret": reinjection, "domains": rewritePatterns(snapshot), "rule_version": snapshot.Version(), "block_authorized_quic": true}
	backend, e := readBackend(paths)
	if e != nil {
		_ = rewriteDeleteGeneration(client, generation)
		return e
	}
	oldRun, oldErr := readRewriteRun(paths)
	run := rewriteRun{Version: 1, Profile: name, Backend: backend, Generation: generation, EngineEpoch: current.EngineEpoch, RuleVersion: snapshot.Version(), PID: worker.PID(), CreatedAt: rewriteNow(), RenewAt: rewriteNow().Add(24*time.Hour - 6*time.Minute)}
	if oldErr == nil {
		oldRun.Previous = nil
		run.Previous = &oldRun
	}
	if e = writeRewriteRun(paths, run); e != nil {
		_ = rewriteDeleteGeneration(client, generation)
		return e
	}
	if e = rewriteStartSupervisor(paths, generation); e != nil {
		_ = writeRewriteRun(paths, oldRun)
		return rewriteFailure("rewrite_activation_failed")
	}
	var prepared struct {
		EngineEpoch         string `json:"engine_epoch"`
		ReinjectionAddr     string `json:"reinjection_addr"`
		EngineTransportCert string `json:"engine_transport_cert"`
	}
	if e = client.Put(ctx, "/request-rewrite/session", request, &prepared); e != nil {
		published = true // supervisor retains candidate ownership after ambiguous I/O
		return rewriteFailure("rewrite_state_uncertain")
	}
	run.EngineEpoch = prepared.EngineEpoch
	if e = writeRewriteRun(paths, run); e != nil {
		return rewriteFailure("rewrite_state_uncertain")
	}
	configured := child.Configuration{Generation: generation, EngineEpoch: prepared.EngineEpoch, ReinjectionAddr: prepared.ReinjectionAddr, EngineTransportCert: prepared.EngineTransportCert, HandoffSecret: handoff, ReinjectionSecret: reinjection}
	if e = worker.Configure(configured); e != nil {
		_ = rewriteDeleteGeneration(client, generation)
		return rewriteFailure("rewrite_activation_failed")
	}
	var probe struct {
		Ready bool `json:"ready"`
	}
	if e = client.Post(ctx, "/request-rewrite/probe", map[string]any{"version": 1, "generation": generation}, &probe); e != nil || !probe.Ready {
		_ = rewriteDeleteGeneration(client, generation)
		return rewriteFailure("rewrite_activation_failed")
	}
	// Install a non-sensitive recovery record before publication. The record is
	// preserved on uncertainty; it never substitutes for engine status evidence.
	mutationCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	var active rewriteEngineStatus
	e = client.Put(mutationCtx, "/request-rewrite/branch", map[string]any{"version": 1, "generation": generation, "enabled": true}, &active)
	cancel()
	if e != nil || active.Generation != generation || !active.BranchEnabled || active.RuleVersion != snapshot.Version() {
		checkCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if client.Get(checkCtx, "/request-rewrite/status", &active) != nil {
			published = true
			return rewriteFailure("rewrite_state_uncertain")
		}
		if active.Generation != generation || !active.BranchEnabled {
			_ = rewriteDeleteGeneration(client, generation)
			if oldErr == nil {
				_ = writeRewriteRun(paths, oldRun)
			}
			return rewriteFailure("rewrite_activation_failed")
		}
	}
	published = true
	// CAS publication revokes the old contexts atomically. Shutdown uses the
	// authenticated generation socket, never a PID read from a mutable file.
	if previous != nil {
		_ = rewriteShutdownChild(paths, *previous)
	}
	run.Previous = nil
	if e = writeRewriteRun(paths, run); e != nil {
		return rewriteFailure("rewrite_state_uncertain")
	}
	return nil
}
func rewritePatterns(snapshot *rewrite.Snapshot) []string {
	out := []string{}
	for _, d := range snapshot.Document().Domains {
		out = append(out, d.Pattern)
	}
	return out
}
func rewriteRandom(n int, token bool) (string, error) {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		return "", rewriteFailure("rewrite_unavailable")
	}
	if token {
		return base64.RawURLEncoding.EncodeToString(b), nil
	}
	return hex.EncodeToString(b), nil
}
func rewriteSocket(paths nagiruntime.Paths, generation string) string {
	return filepath.Join(paths.RuntimeDir, "rewrite-"+generation+".sock")
}
func rewriteDeleteGeneration(client *control.Client, generation string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := client.Delete(ctx, "/request-rewrite/session?generation="+generation, nil); e != nil {
		return e
	}
	var state rewriteEngineStatus
	if e := client.Get(ctx, "/request-rewrite/status", &state); e != nil {
		return e
	}
	if state.Generation == generation || state.PreparedGeneration == generation {
		return rewriteFailure("rewrite_state_uncertain")
	}
	return nil
}
func rewriteShutdownChild(paths nagiruntime.Paths, generation string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, e := child.Control(ctx, rewriteSocket(paths, generation), "POST", "/shutdown", generation)
	if e != nil {
		if _, stat := os.Lstat(rewriteSocket(paths, generation)); os.IsNotExist(stat) {
			return nil
		}
		return rewriteFailure("rewrite_state_uncertain")
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, e := os.Lstat(rewriteSocket(paths, generation)); os.IsNotExist(e) {
			return nil
		}
		select {
		case <-ctx.Done():
			return rewriteFailure("rewrite_state_uncertain")
		case <-ticker.C:
		}
	}
}

// Disable removes interception first, then drains/closes the child and engine
// session. Failure to prove removal never yields a successful disabled result.
func rewriteTeardown(ctx context.Context, paths nagiruntime.Paths) error {
	run, runErr := readRewriteRun(paths)
	client, e := controlClientForCurrentBackend(paths)
	if e != nil {
		if os.IsNotExist(runErr) {
			return nil
		}
		return rewriteFailure("rewrite_state_uncertain")
	}
	var state rewriteEngineStatus
	e = client.Get(ctx, "/request-rewrite/status", &state)
	if e != nil {
		if os.IsNotExist(runErr) {
			return nil
		}
		// An absent user socket proves this backend has no controller. Do not use
		// arbitrary API/transport failures as proof that a root engine exited.
		backend, _ := readBackend(paths)
		if backend == userBackend {
			if _, stat := os.Lstat(paths.SocketPath); os.IsNotExist(stat) {
				if e = rewriteShutdownChild(paths, run.Generation); e != nil {
					return e
				}
				return removeRewriteRun(paths)
			}
		}
		return rewriteFailure("rewrite_state_uncertain")
	}
	generation := state.Generation
	if state.BranchEnabled {
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		e = client.Put(bounded, "/request-rewrite/branch", map[string]any{"version": 1, "generation": generation, "enabled": false}, &state)
		cancel()
		if e != nil || state.BranchEnabled {
			check, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if client.Get(check, "/request-rewrite/status", &state) != nil || state.BranchEnabled {
				return rewriteFailure("rewrite_state_uncertain")
			}
		}
	}
	if state.PreparedGeneration != "" {
		candidate := state.PreparedGeneration
		if e = rewriteDeleteGeneration(client, candidate); e != nil {
			return rewriteFailure("rewrite_state_uncertain")
		}
		if e = rewriteShutdownChild(paths, candidate); e != nil {
			return e
		}
	}
	if generation != "" {
		drain, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, _ = child.Control(drain, rewriteSocket(paths, generation), "POST", "/drain", generation)
		cancel()
		if e = rewriteDeleteGeneration(client, generation); e != nil {
			return rewriteFailure("rewrite_state_uncertain")
		}
		if e = rewriteShutdownChild(paths, generation); e != nil {
			return e
		}
	}
	if runErr == nil && run.Generation != generation {
		if e = rewriteShutdownChild(paths, run.Generation); e != nil {
			return e
		}
	}
	return removeRewriteRun(paths)
}
