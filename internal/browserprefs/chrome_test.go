package browserprefs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func ptr(v string) *string { return &v }

type fake struct {
	p         preference
	writes    int
	failWrite bool
	race      bool
}

func (f *fake) run(_ context.Context, name string, args ...string) (string, error) {
	if name != "/usr/bin/osascript" || len(args) != 7 || args[0] != "-l" || args[1] != "JavaScript" || args[2] != "-e" || args[3] != chromeScript {
		return "", errors.New("unexpected invocation")
	}
	if args[4] == "write" {
		f.writes++
		if f.failWrite {
			return "", errors.New("write failed")
		}
		var expected, target *string
		if err := json.Unmarshal([]byte(args[5]), &expected); err != nil {
			return "", err
		}
		if err := json.Unmarshal([]byte(args[6]), &target); err != nil {
			return "", err
		}
		if f.race {
			f.p.Value = ptr("default_public_interface_only")
			f.race = false
		}
		if !f.p.Forced && equal(f.p.Value, expected) {
			f.p.Value = target
		}
	}
	b, _ := json.Marshal(f.p)
	return string(b), nil
}
func manager(t *testing.T, value *string) (*Manager, *fake) {
	t.Helper()
	f := &fake{p: preference{Value: value, Valid: true}}
	return &Manager{Dir: t.TempDir(), OS: "darwin", Run: f.run}, f
}
func TestEnableRestorePreservesValuesAndPrivateJournal(t *testing.T) {
	for _, before := range []*string{nil, ptr("default"), ptr("default_public_and_private_interfaces"), ptr("default_public_interface_only")} {
		t.Run(stringValue(before), func(t *testing.T) {
			m, f := manager(t, before)
			ctx := context.Background()
			st, err := m.Enable(ctx)
			if err != nil || !st.Restricted || !st.Managed || !st.CanRestore || !st.RequiresRestart {
				t.Fatalf("enable: %+v %v", st, err)
			}
			info, err := os.Stat(m.path())
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("journal permissions: %v %v", info, err)
			}
			first, err := m.saved()
			if err != nil || !equal(first.Before, before) {
				t.Fatalf("before: %+v %v", first, err)
			}
			st, err = m.Enable(ctx)
			if err != nil || st.RequiresRestart || f.writes != 1 {
				t.Fatalf("idempotent enable: %+v %v writes=%d", st, err, f.writes)
			}
			st, err = m.Restore(ctx)
			if err != nil || !equal(st.CurrentValue, before) || st.Managed || !st.RequiresRestart {
				t.Fatalf("restore: %+v %v", st, err)
			}
			if _, err = os.Stat(m.path()); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("journal still exists: %v", err)
			}
			if _, err = m.Restore(ctx); err != nil || f.writes != 2 {
				t.Fatalf("idempotent restore: %v writes=%d", err, f.writes)
			}
		})
	}
}
func stringValue(v *string) string {
	if v == nil {
		return "absent"
	}
	return *v
}
func TestPreexistingRestrictionIsNotClaimedOrRemoved(t *testing.T) {
	m, f := manager(t, ptr(RestrictedValue))
	ctx := context.Background()
	for _, op := range []func(context.Context) (State, error){m.Status, m.Enable, m.Restore} {
		s, err := op(ctx)
		if err != nil || !s.Restricted || s.Managed || s.CanEnable || s.CanRestore || s.RequiresRestart {
			t.Fatalf("state=%+v err=%v", s, err)
		}
	}
	if f.writes != 0 {
		t.Fatal("external preference changed")
	}
}
func TestForcedAndInvalidValuesCannotBeWritten(t *testing.T) {
	m, f := manager(t, nil)
	f.p.Forced = true
	s, err := m.Status(context.Background())
	if err != nil || !s.Forced || s.CanEnable || s.CanRestore {
		t.Fatalf("state=%+v err=%v", s, err)
	}
	if _, err = m.Enable(context.Background()); err == nil {
		t.Fatal("forced enable accepted")
	}
	if f.writes != 0 {
		t.Fatal("forced value changed")
	}
	f.p.Forced = false
	f.p.Valid = false
	if _, err = m.Enable(context.Background()); err == nil {
		t.Fatal("invalid value accepted")
	}
	f.p.Valid = true
	f.p.Value = ptr("unknown")
	if _, err = m.Enable(context.Background()); err == nil {
		t.Fatal("unknown value accepted")
	}
}
func TestExternalChangePreservedAndConflictJournalRetained(t *testing.T) {
	m, f := manager(t, nil)
	ctx := context.Background()
	if _, err := m.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	f.p.Value = ptr("default_public_interface_only")
	s, err := m.Status(ctx)
	if err != nil || !s.Managed || s.CanEnable || s.CanRestore {
		t.Fatalf("conflict status=%+v %v", s, err)
	}
	for _, op := range []func(context.Context) (State, error){m.Enable, m.Restore} {
		if _, err = op(ctx); err == nil {
			t.Fatal("external change overwritten")
		}
	}
	if _, err = os.Stat(m.path()); err != nil {
		t.Fatal("lost journal")
	}
	if f.writes != 1 {
		t.Fatal("unexpected write")
	}
}
func TestConcurrentPreferenceChangeCannotBeOverwritten(t *testing.T) {
	m, f := manager(t, nil)
	f.race = true
	if _, err := m.Enable(context.Background()); err == nil {
		t.Fatal("concurrent change accepted")
	}
	if *f.p.Value != "default_public_interface_only" {
		t.Fatal("concurrent change overwritten")
	}
	if _, err := os.Stat(m.path()); err != nil {
		t.Fatal("lost recovery record")
	}
}
func TestFailedEnableCanRetryOrRestoreWithoutLosingOriginal(t *testing.T) {
	m, f := manager(t, nil)
	ctx := context.Background()
	f.failWrite = true
	if _, err := m.Enable(ctx); err == nil {
		t.Fatal("failed write accepted")
	}
	s, err := m.Status(ctx)
	if err != nil || !s.Managed || !s.CanEnable || !s.CanRestore {
		t.Fatalf("pending state=%+v %v", s, err)
	}
	f.failWrite = false
	s, err = m.Restore(ctx)
	if err != nil || s.Managed || s.CurrentValue != nil {
		t.Fatalf("restore failed enable: %+v %v", s, err)
	}
}
func TestFailedRestoreKeepsRecoveryRecord(t *testing.T) {
	m, f := manager(t, nil)
	ctx := context.Background()
	if _, err := m.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	f.failWrite = true
	if _, err := m.Restore(ctx); err == nil {
		t.Fatal("failed restore accepted")
	}
	if _, err := os.Stat(m.path()); err != nil {
		t.Fatal("lost recovery record")
	}
}
func TestUnsupportedPlatformAndInvalidJournal(t *testing.T) {
	m, _ := manager(t, nil)
	m.OS = "linux"
	if _, err := m.Status(context.Background()); err == nil {
		t.Fatal("unsupported platform accepted")
	}
	m.OS = "darwin"
	if err := os.WriteFile(m.path(), []byte(`{"version":1,"applied":"unexpected"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Status(context.Background()); err == nil {
		t.Fatal("corrupt journal accepted")
	}
}
func TestStatusDoesNotChangePreferenceAndJSONFieldsAreStable(t *testing.T) {
	m, f := manager(t, nil)
	s, err := m.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if f.writes != 0 {
		t.Fatal("status writes")
	}
	b, _ := json.Marshal(s)
	var fields map[string]any
	_ = json.Unmarshal(b, &fields)
	want := map[string]any{"available": true, "current_value": nil, "restricted": false, "managed": false, "forced": false, "can_enable": true, "can_disable": false, "can_restore": false, "requires_restart": false}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("got %s", b)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "chrome-webrtc.lock")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(chromeScript, "current.value!==expected") {
		t.Fatal("lost native compare guard")
	}
}

func TestDisableRemovesManualRestrictionAndCanEnableAgain(t *testing.T) {
	m, f := manager(t, ptr(RestrictedValue))
	ctx := context.Background()
	s, err := m.Status(ctx)
	if err != nil || !s.CanDisable || s.Managed {
		t.Fatalf("manual status: %+v %v", s, err)
	}
	s, err = m.Disable(ctx)
	if err != nil || s.CurrentValue != nil || s.Restricted || s.Managed || !s.CanEnable || s.CanDisable || !s.RequiresRestart {
		t.Fatalf("disable manual: %+v %v", s, err)
	}
	if _, err = m.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	if f.p.Value == nil || *f.p.Value != RestrictedValue {
		t.Fatal("could not enable after manual restriction was disabled")
	}
}
func TestDisableDiscardsSavedValueInsteadOfRestoringIt(t *testing.T) {
	m, _ := manager(t, ptr("default_public_interface_only"))
	ctx := context.Background()
	if _, err := m.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	s, err := m.Disable(ctx)
	if err != nil || s.CurrentValue != nil || s.Managed || !s.CanEnable {
		t.Fatalf("disable: %+v %v", s, err)
	}
	if _, err := os.Stat(m.path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal not cleared: %v", err)
	}
	if _, err = m.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	s, err = m.Enable(ctx)
	if err != nil || !s.Restricted {
		t.Fatalf("re-enable: %+v %v", s, err)
	}
	j, err := m.saved()
	if err != nil || j.Before != nil {
		t.Fatalf("new baseline reused discarded preference: %+v %v", j, err)
	}
}
func TestDisableClearsStaleJournalAndIsIdempotent(t *testing.T) {
	m, f := manager(t, nil)
	ctx := context.Background()
	if _, err := m.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	f.p.Value = ptr("default_public_interface_only")
	s, err := m.Disable(ctx)
	if err != nil || s.Managed || s.CurrentValue != nil || !s.CanEnable {
		t.Fatalf("disable stale: %+v %v", s, err)
	}
	writes := f.writes
	s, err = m.Disable(ctx)
	if err != nil || s.RequiresRestart || f.writes != writes {
		t.Fatalf("idempotent disable: %+v %v", s, err)
	}
}
func TestDisableHonorsForcedAndConcurrentChanges(t *testing.T) {
	m, f := manager(t, ptr(RestrictedValue))
	ctx := context.Background()
	f.p.Forced = true
	s, err := m.Status(ctx)
	if err != nil || s.CanDisable {
		t.Fatalf("forced status: %+v %v", s, err)
	}
	if _, err = m.Disable(ctx); err == nil || f.writes != 0 {
		t.Fatal("forced value was deleted")
	}
	f.p.Forced = false
	f.race = true
	if _, err = m.Disable(ctx); err == nil {
		t.Fatal("concurrent change accepted")
	}
	if !equal(f.p.Value, ptr("default_public_interface_only")) {
		t.Fatal("concurrent change overwritten")
	}
}
func TestFailedDisableKeepsRestrictionAndRecoveryRecord(t *testing.T) {
	m, f := manager(t, nil)
	ctx := context.Background()
	if _, err := m.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	f.failWrite = true
	if _, err := m.Disable(ctx); err == nil {
		t.Fatal("failed disable accepted")
	}
	if !equal(f.p.Value, ptr(RestrictedValue)) {
		t.Fatal("failed disable changed preference")
	}
	if _, err := os.Stat(m.path()); err != nil {
		t.Fatal("lost recovery record")
	}
	f.failWrite = false
	s, err := m.Disable(ctx)
	if err != nil || s.Managed || !s.CanEnable {
		t.Fatalf("retry disable: %+v %v", s, err)
	}
}
