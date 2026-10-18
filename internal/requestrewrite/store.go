package requestrewrite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

func Path(config, profile string) string {
	return filepath.Join(config, "request-rewrite", profile+".json")
}

type Store struct {
	ConfigDir, Profile string
	Activation         Activation
}

// Activation is supplied by the lifecycle coordinator. Apply/Rollback must
// confirm runtime state (fresh generations when required), or return an error.
// No runtime activation is implemented by this domain package.
type Activation interface {
	Apply(context.Context, *Snapshot, *Snapshot) error
	Rollback(context.Context, *Snapshot) error
}
type SaveResult struct {
	Profile               string `json:"profile"`
	Saved                 bool   `json:"saved"`
	ConfiguredRuleVersion string `json:"configured_rule_version"`
	RuntimeConfirmed      bool   `json:"runtime_confirmed"`
}

func (s Store) Load() (*Snapshot, error) {
	if !validID(s.Profile) {
		return nil, invalid()
	}
	d, e := openPrivate(s.ConfigDir, false, false)
	if errors.Is(e, os.ErrNotExist) {
		return Empty(), nil
	}
	if e != nil {
		return nil, e
	}
	defer d.close()
	return load(d, s.Profile)
}
func load(d *privateDir, profile string) (*Snapshot, error) {
	b, e := d.read(profile+".json", MaxDocumentBytes)
	if errors.Is(e, os.ErrNotExist) {
		return Empty(), nil
	}
	if e != nil {
		return nil, e
	}
	return Parse(b)
}

// Save requires the caller's ordinary configuration lock before this internal
// document lock. Enabled intent cannot be changed without a runtime coordinator.
// Runtime failures restore disk and request confirmed runtime rollback.
func (s Store) Save(ctx context.Context, candidate *Snapshot) (SaveResult, error) {
	return s.save(ctx, candidate, "")
}
func (s Store) save(ctx context.Context, candidate *Snapshot, expected string) (SaveResult, error) {
	out := SaveResult{Profile: s.Profile}
	if !validID(s.Profile) || candidate == nil {
		return out, invalid()
	}
	validated, validationErr := Parse(candidate.canonical)
	if validationErr != nil {
		return out, validationErr
	}
	candidate = validated
	d, e := openPrivate(s.ConfigDir, false, true)
	if e != nil {
		return out, e
	}
	defer d.close()
	unlock, e := d.lock()
	if e != nil {
		return out, e
	}
	defer unlock()
	oldBytes, oldErr := d.read(s.Profile+".json", MaxDocumentBytes)
	if oldErr != nil && !errors.Is(oldErr, os.ErrNotExist) {
		return out, oldErr
	}
	old := Empty()
	if oldErr == nil {
		old, e = Parse(oldBytes)
		if e != nil {
			return out, e
		}
	}
	if expected != "" && old.version != expected {
		return out, &Error{Code: "rewrite_conflict"}
	}
	if s.Activation == nil && (old.doc.Enabled || candidate.doc.Enabled) {
		return out, &Error{Code: "rewrite_activation_required"}
	}
	if e = ctx.Err(); e != nil {
		return out, &Error{Code: "rewrite_cancelled"}
	}
	if e = d.write(s.Profile+".json", candidate.canonical, false); e != nil {
		return out, e
	}
	if s.Activation != nil {
		if e = s.Activation.Apply(ctx, old, candidate); e != nil {
			var restore error
			if errors.Is(oldErr, os.ErrNotExist) {
				restore = d.remove(s.Profile + ".json")
			} else {
				restore = d.write(s.Profile+".json", oldBytes, false)
			}
			rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			rollback := s.Activation.Rollback(rollbackCtx, old)
			cancel()
			if restore != nil || rollback != nil {
				return out, &Error{Code: "rewrite_state_uncertain"}
			}
			return out, &Error{Code: "rewrite_activation_failed"}
		}
		out.RuntimeConfirmed = true
	}
	out.Saved = true
	out.ConfiguredRuleVersion = candidate.version
	return out, nil
}

// ImportSensitive rejects enable from a portable document and independently
// retains the profile's existing intent. Activation follows the same transaction.
func (s Store) ImportSensitive(ctx context.Context, data []byte) (SaveResult, error) {
	candidate, e := Parse(data)
	if e != nil {
		return SaveResult{}, e
	}
	if candidate.doc.Enabled {
		return SaveResult{}, invalid()
	}
	old, e := s.Load()
	if e != nil {
		return SaveResult{}, e
	}
	d := candidate.Document()
	d.Enabled = old.doc.Enabled
	candidate, e = Compile(d)
	if e != nil {
		return SaveResult{}, e
	}
	return s.save(ctx, candidate, old.version)
}

// RemoveProfile is for profile deletion under the configuration transaction.
// It never touches CA storage. Profile IDs cannot address the CA directory.
func RemoveProfile(config, profile string) error {
	if !validID(profile) {
		return invalid()
	}
	d, e := openPrivate(config, false, false)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	defer d.close()
	unlock, e := d.lock()
	if e != nil {
		return e
	}
	defer unlock()
	return d.remove(profile + ".json")
}
