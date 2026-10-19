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
	Version             int         `json:"version"`
	Profile             string      `json:"profile"`
	Backend             string      `json:"backend"`
	Generation          string      `json:"generation"`
	EngineEpoch         string      `json:"engine_epoch"`
	RuleVersion         string      `json:"rule_version"`
	PID                 int         `json:"pid"`
	OwnerUID            int         `json:"owner_uid"`
	StartIdentity       string      `json:"start_identity"`
	RenewAt             time.Time   `json:"renew_at"`
	CreatedAt           time.Time   `json:"created_at"`
	Previous            *rewriteRun `json:"previous,omitempty"`
	EnginePID           int         `json:"engine_pid"`
	EngineStartIdentity string      `json:"engine_start_identity"`
}

func rewriteFailure(code string) error {
	next := "run `nagi request-rewrite status` or `nagi request-rewrite disable`"
	switch code {
	case "rewrite_capability_missing":
		next = "a matching engine update is required; use `nagi update` once published, or build the reviewed development engine as documented in docs/development/build-and-test.md#request-rewrite-coordinator"
	case "rewrite_tun_required":
		next = "run `nagi tun enable`, then retry `nagi request-rewrite enable`"
	case "rewrite_ca_required":
		next = "run `nagi request-rewrite ca generate`, then explicitly install trust"
	case "rewrite_ca_trust_required":
		next = "run `nagi request-rewrite ca trust install`; on Linux export the public CA and use your distribution/application trust settings"
	}
	return fail("request_rewrite_error", fmt.Errorf("%s; %s", code, next))
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
		code := "rewrite_invalid"
		return RewriteStatus{Actual: "unavailable", LastError: &code, Recovery: "nagi profile list"}, nil
	}
	return rewriteStatusFor(ctx, paths, name)
}
func rewriteStatusFor(ctx context.Context, paths nagiruntime.Paths, name string) (RewriteStatus, error) {
	out := RewriteStatus{Profile: name, Actual: "unavailable", Recovery: "nagi request-rewrite disable"}
	snapshot, err := (rewrite.Store{ConfigDir: paths.ConfigDir, Profile: name}).Load()
	if err != nil {
		code := "rewrite_invalid"
		out.LastError = &code
		return out, nil
	}
	out.EnabledIntent = snapshot.Document().Enabled
	out.ConfiguredRuleVersion = snapshot.Version()
	out.CA, _ = requestRewriteCAStatus(paths)
	out.Backend, _ = readBackend(paths)
	client, err := rewriteClient(paths)
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
			out.Recovery = "Build the reviewed development engine: docs/development/build-and-test.md#request-rewrite-coordinator"
		}
		out.LastError = &code
		return out, nil
	}
	if state.Version != control.RewriteProtocolVersion {
		code := "rewrite_capability_missing"
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
		health, e := rewriteChildControl(ctx, rewriteSocket(paths, state.Generation), "GET", "/status", state.Generation)
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
		if out.EnabledIntent && !snapshot.HasEnabledRules() {
			out.Actual = "idle"
			out.Recovery = ""
		}
	}
	if out.Actual == "active" && out.EnabledIntent && out.ActiveRuleVersion == out.ConfiguredRuleVersion {
		out.Recovery = ""
	}
	return out, nil
}

func requestRewriteSetEnabled(ctx context.Context, paths nagiruntime.Paths, enabled bool) (RewriteStatus, error) {
	name, e := rewriteSelected(paths)
	if e != nil {
		if !enabled {
			if e = rewriteTeardown(ctx, paths); e != nil {
				return RewriteStatus{}, e
			}
			return RewriteStatus{Actual: "disabled", Recovery: "nagi profile list"}, rewriteFailure("rewrite_invalid")
		}
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
	if enabled {
		candidate, e := store.Load()
		if e != nil {
			return RewriteStatus{}, rewriteDomainError(e)
		}
		if candidate.HasEnabledRules() {
			if _, _, e = rewritePrerequisites(ctx, paths, candidate); e != nil {
				return RewriteStatus{}, e
			}
		}
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
	if !candidate.Document().Enabled || !candidate.HasEnabledRules() {
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
	if !old.Document().Enabled || !old.HasEnabledRules() {
		return rewriteTeardown(ctx, a.paths)
	}
	// A rejected candidate can leave the previous active generation untouched.
	// Confirm it before restoring ownership; never restart/replay a mutation.
	if run, err := readRewriteRun(a.paths); err == nil && run.Previous != nil {
		client, err := rewriteClient(a.paths)
		if err != nil {
			return err
		}
		var current rewriteEngineStatus
		if client.Get(ctx, "/request-rewrite/status", &current) != nil {
			return rewriteFailure("rewrite_state_uncertain")
		}
		if current.Generation == run.Previous.Generation && current.RuleVersion == old.Version() && current.BranchEnabled {
			if rewriteDeleteGeneration(client, run.Generation) != nil {
				return rewriteFailure("rewrite_state_uncertain")
			}
			if rewriteShutdownRun(ctx, a.paths, run) != nil {
				return rewriteFailure("rewrite_state_uncertain")
			}
			previous := *run.Previous
			if writeRewriteRun(a.paths, previous) != nil {
				return rewriteFailure("rewrite_state_uncertain")
			}
			return rewriteStartSupervisor(a.paths, previous.Generation)
		}
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
	if !snapshot.Document().Enabled || !snapshot.HasEnabledRules() {
		return rewriteLifecycleStop(ctx, paths)
	}
	backend, backendErr := readBackend(paths)
	if backendErr != nil {
		return backendErr
	}
	if backend == userBackend {
		if _, e := os.Lstat(paths.SocketPath); os.IsNotExist(e) {
			return nil
		}
	}
	if backend == privilegedBackend {
		state, e := (helperBackend{paths: paths}).Status(ctx)
		if e == nil && !state.Running {
			return nil
		}
	}
	return rewriteActivate(ctx, paths, name, snapshot)
}

type managedRewriteChild interface {
	Announcement() child.Announcement
	Configure(child.Configuration) error
	Close() error
	PID() int
}
type rewriteProcess struct{ *child.Child }

func (c rewriteProcess) Announcement() child.Announcement { return c.Child.Announcement }

var rewriteStartChild = func(ctx context.Context, executable string, options child.ChildOptions) (managedRewriteChild, error) {
	c, e := child.StartChild(ctx, executable, options)
	if e != nil {
		return nil, e
	}
	return rewriteProcess{c}, nil
}
var rewriteEffectiveUID = os.Geteuid
var rewriteChildControl = child.Control
var rewriteClient = controlClientForCurrentBackend
var rewriteNow = time.Now
var rewriteStartSupervisor = startRewriteSupervisor

func rewritePrerequisites(ctx context.Context, paths nagiruntime.Paths, snapshot *rewrite.Snapshot) (*control.Client, rewriteEngineStatus, error) {
	if rewriteEffectiveUID() == 0 {
		return nil, rewriteEngineStatus{}, rewriteFailure("rewrite_auth")
	}
	if !snapshot.HasEnabledRules() {
		return nil, rewriteEngineStatus{}, rewriteFailure("rewrite_invalid")
	}
	client, e := rewriteClient(paths)
	if e != nil {
		return nil, rewriteEngineStatus{}, rewriteFailure("rewrite_unavailable")
	}
	var capabilities control.RewriteCapabilities
	if e = client.Get(ctx, "/request-rewrite/capabilities", &capabilities); e != nil {
		return nil, rewriteEngineStatus{}, rewriteFailure("rewrite_capability_missing")
	}
	if !capabilities.Supported() {
		return nil, rewriteEngineStatus{}, rewriteFailure("rewrite_capability_missing")
	}
	var current rewriteEngineStatus
	if client.Get(ctx, "/request-rewrite/status", &current) != nil {
		return nil, rewriteEngineStatus{}, rewriteFailure("rewrite_unavailable")
	}
	if current.Version != control.RewriteProtocolVersion {
		return nil, rewriteEngineStatus{}, rewriteFailure("rewrite_capability_missing")
	}
	if !current.TUNRunning {
		return nil, rewriteEngineStatus{}, rewriteFailure("rewrite_tun_required")
	}
	ca, e := requestRewriteCAStatus(paths)
	if e != nil || ca.State != "generated" {
		return nil, rewriteEngineStatus{}, rewriteFailure("rewrite_ca_required")
	}
	if ca.Trust != "trusted" {
		return nil, rewriteEngineStatus{}, rewriteFailure("rewrite_ca_trust_required")
	}

	return client, current, nil
}

func rewriteActivate(ctx context.Context, paths nagiruntime.Paths, name string, snapshot *rewrite.Snapshot) error {
	if !snapshot.HasEnabledRules() {
		return rewriteTeardown(ctx, paths)
	}
	client, current, e := rewritePrerequisites(ctx, paths, snapshot)
	if e != nil {
		return e
	}
	if run, e := readRewriteRun(paths); e == nil && current.State == "active" && run.Profile == name && current.RuleVersion == snapshot.Version() && run.Generation == current.Generation && rewriteNow().Before(run.RenewAt) {
		health, e := rewriteChildControl(ctx, rewriteSocket(paths, run.Generation), "GET", "/status", run.Generation)
		if e == nil && health.Ready && health.RuleVersion == snapshot.Version() {
			return rewriteStartSupervisor(paths, run.Generation)
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
	request := control.RewritePrepare{Version: control.RewriteProtocolVersion, Generation: generation, PreviousGeneration: previous, OwnerUID: uint32(rewriteEffectiveUID()), HandoffAddr: worker.Announcement().HandoffAddr, ChildTransportCert: worker.Announcement().ChildTransportCert, HandoffSecret: handoff, ReinjectionSecret: reinjection, InterceptDomains: snapshot.InterceptDomains(), TargetDomains: snapshot.TargetDomains(), RuleVersion: snapshot.Version(), BlockAuthorizedQUIC: true}
	backend, e := readBackend(paths)
	if e != nil {
		_ = rewriteDeleteGeneration(client, generation)
		return e
	}
	oldRun, oldErr := readRewriteRun(paths)
	enginePID, engineIdentity, e := rewriteEngineIdentity(ctx, paths)
	if e != nil {
		return rewriteFailure("rewrite_state_uncertain")
	}
	identity, e := rewriteProcessIdentity(worker.PID())
	if e != nil {
		return rewriteFailure("rewrite_activation_failed")
	}
	run := rewriteRun{EnginePID: enginePID, EngineStartIdentity: engineIdentity, OwnerUID: rewriteEffectiveUID(), StartIdentity: identity, Version: control.RewriteProtocolVersion, Profile: name, Backend: backend, Generation: generation, EngineEpoch: current.EngineEpoch, RuleVersion: snapshot.Version(), PID: worker.PID(), CreatedAt: rewriteNow(), RenewAt: rewriteNow().Add(24*time.Hour - 6*time.Minute)}
	if oldErr == nil {
		oldRun.Previous = nil
		run.Previous = &oldRun
	}
	if e = writeRewriteRun(paths, run); e != nil {
		_ = rewriteDeleteGeneration(client, generation)
		return e
	}
	if e = rewriteStartSupervisor(paths, generation); e != nil {
		if oldErr == nil {
			if writeRewriteRun(paths, oldRun) != nil {
				return rewriteFailure("rewrite_state_uncertain")
			}
		} else {
			if removeRewriteRun(paths) != nil {
				return rewriteFailure("rewrite_state_uncertain")
			}
		}
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
	if e = client.Post(ctx, "/request-rewrite/probe", map[string]any{"version": control.RewriteProtocolVersion, "generation": generation}, &probe); e != nil || !probe.Ready {
		_ = rewriteDeleteGeneration(client, generation)
		return rewriteFailure("rewrite_activation_failed")
	}
	// Install a non-sensitive recovery record before publication. The record is
	// preserved on uncertainty; it never substitutes for engine status evidence.
	mutationCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	var active rewriteEngineStatus
	e = client.Put(mutationCtx, "/request-rewrite/branch", map[string]any{"version": control.RewriteProtocolVersion, "generation": generation, "enabled": true}, &active)
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
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if oldErr == nil && oldRun.Generation == *previous {
			e = rewriteShutdownRun(cleanup, paths, oldRun)
		} else {
			e = rewriteShutdownChildContext(cleanup, paths, *previous)
		}
		cancel()
		if e != nil {
			return rewriteFailure("rewrite_state_uncertain")
		}
	}
	run.Previous = nil
	if e = writeRewriteRun(paths, run); e != nil {
		return rewriteFailure("rewrite_state_uncertain")
	}
	return nil
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
	return rewriteDeleteGenerationContext(ctx, client, generation)
}
func rewriteDeleteGenerationContext(ctx context.Context, client *control.Client, generation string) error {
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
	return rewriteShutdownChildContext(ctx, paths, generation)
}
func rewriteShutdownChildContext(ctx context.Context, paths nagiruntime.Paths, generation string) error {
	_, e := rewriteChildControl(ctx, rewriteSocket(paths, generation), "POST", "/shutdown", generation)
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
	ctx, cancelCleanup := context.WithTimeout(ctx, 5*time.Second)
	defer cancelCleanup()
	run, runErr := readRewriteRun(paths)
	client, e := rewriteClient(paths)
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
		// A missing socket is not proof of process exit. Compare the captured
		// process start identity or an authenticated helper stopped response.
		if runErr == nil && rewriteEngineExited(ctx, paths, run) {
			if e = rewriteShutdownRun(ctx, paths, run); e != nil {
				return e
			}
			if run.Previous != nil {
				if e = rewriteShutdownRun(ctx, paths, *run.Previous); e != nil {
					return e
				}
			}
			return removeRewriteRun(paths)
		}
		return rewriteFailure("rewrite_state_uncertain")
	}
	generation := state.Generation
	if state.BranchEnabled {
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		e = client.Put(bounded, "/request-rewrite/branch", map[string]any{"version": control.RewriteProtocolVersion, "generation": generation, "enabled": false}, &state)
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
		if e = rewriteDeleteGenerationContext(ctx, client, candidate); e != nil {
			return rewriteFailure("rewrite_state_uncertain")
		}
		if runErr == nil && run.Generation == candidate {
			e = rewriteShutdownRun(ctx, paths, run)
		} else if runErr == nil && run.Previous != nil && run.Previous.Generation == candidate {
			e = rewriteShutdownRun(ctx, paths, *run.Previous)
		} else {
			e = rewriteShutdownChildContext(ctx, paths, candidate)
		}
		if e != nil {
			return e
		}
	}
	if generation != "" {
		drain, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, _ = rewriteChildControl(drain, rewriteSocket(paths, generation), "POST", "/drain", generation)
		cancel()
		if e = rewriteDeleteGenerationContext(ctx, client, generation); e != nil {
			return rewriteFailure("rewrite_state_uncertain")
		}
		if runErr == nil && run.Generation == generation {
			e = rewriteShutdownRun(ctx, paths, run)
		} else if runErr == nil && run.Previous != nil && run.Previous.Generation == generation {
			e = rewriteShutdownRun(ctx, paths, *run.Previous)
		} else {
			e = rewriteShutdownChildContext(ctx, paths, generation)
		}
		if e != nil {
			return e
		}
	}
	if runErr == nil && run.Generation != generation {
		if e = rewriteShutdownRun(ctx, paths, run); e != nil {
			return e
		}
	}
	if runErr == nil && run.Previous != nil && run.Previous.Generation != generation {
		if e = rewriteShutdownRun(ctx, paths, *run.Previous); e != nil {
			return e
		}
	}
	return removeRewriteRun(paths)
}

// Lifecycle operations need no rewrite API when this feature has never owned
// a generation. Explicit disable always uses rewriteTeardown for recovery.
func rewriteLifecycleStop(ctx context.Context, paths nagiruntime.Paths) error {
	_, e := readRewriteRun(paths)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return rewriteFailure("rewrite_state_uncertain")
	}
	return rewriteTeardown(ctx, paths)
}
func rewriteSwitch(args []string) bool {
	return len(args) > 1 && (args[0] == "profile" && args[1] == "use" || args[0] == "subscription" && args[1] == "apply" || args[0] == "privileged-helper" && (args[1] == "enable" || args[1] == "disable"))
}
func rewriteMutation(args []string) bool {
	if len(args) < 2 || args[0] != "request-rewrite" {
		return false
	}
	switch args[1] {
	case "enable", "disable", "import":
		return true
	case "domains", "rules":
		return len(args) > 2 && args[2] != "list" && args[2] != "show"
	case "ca":
		return len(args) > 2 && (args[2] == "generate" || args[2] == "trust")
	}
	return false
}
