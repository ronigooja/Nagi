package installation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ronigooja/Nagi/internal/output"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func updateErrorPath() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "Nagi", "update-error"), nil
}

// SaveUpdateError records a redacted failure for the next App launch.
func SaveUpdateError(err error) error {
	path, e := updateErrorPath()
	if e != nil {
		return e
	}
	if err == nil {
		return nil
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	message := output.RedactError(err.Error())
	if len(message) > 32768 {
		message = message[:32768]
	}
	if e := removeUpdateErrorMetadata(path); e != nil {
		return e
	}
	if e := os.WriteFile(path, []byte(message), 0600); e != nil {
		return e
	}
	if e := os.Chmod(path, 0600); e != nil {
		return e
	}
	var source *i18n.Message
	if !errors.As(err, &source) || source.Error() != err.Error() {
		return nil
	}
	descriptor := source.Descriptor()
	if !i18n.ValidDescriptor(descriptor) {
		return nil
	}
	descriptor = i18n.CopyDescriptor(descriptor, output.RedactError)
	encoded, e := json.Marshal(updateErrorMetadata{Digest: updateErrorDigest(message), Message: descriptor})
	if e != nil {
		return e
	}
	temp, e := os.CreateTemp(filepath.Dir(path), ".update-error-message-*")
	if e != nil {
		return e
	}
	defer os.Remove(temp.Name())
	if _, e := temp.Write(encoded); e != nil {
		_ = temp.Close()
		return e
	}
	if e := temp.Close(); e != nil {
		return e
	}
	return os.Rename(temp.Name(), path+".messages")
}

type updateErrorMetadata struct {
	Digest  string          `json:"sha256"`
	Message i18n.Descriptor `json:"message"`
}

func updateErrorDigest(message string) string {
	digest := sha256.Sum256([]byte(message))
	return hex.EncodeToString(digest[:])
}
func removeUpdateErrorMetadata(path string) error {
	err := os.Remove(path + ".messages")
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
func readUpdateErrorMessage(path, message string) (i18n.Descriptor, bool) {
	file, err := os.Open(path + ".messages")
	if err != nil {
		return i18n.Descriptor{}, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 128<<10))
	if err != nil {
		return i18n.Descriptor{}, false
	}
	var metadata updateErrorMetadata
	if json.Unmarshal(data, &metadata) != nil || metadata.Digest != updateErrorDigest(message) || !i18n.ValidDescriptor(metadata.Message) {
		return i18n.Descriptor{}, false
	}
	// Explicit null arguments are malformed optional metadata, including no-arg IDs.
	var fields struct {
		Message struct {
			Args json.RawMessage `json:"args"`
		} `json:"message"`
	}
	if json.Unmarshal(data, &fields) != nil || strings.TrimSpace(string(fields.Message.Args)) == "null" {
		return i18n.Descriptor{}, false
	}
	return metadata.Message, true
}

func consumeUpdateError() (map[string]any, error) {
	result, err := consumeUpdateErrorResult()
	if err != nil {
		return nil, err
	}
	return result.Value.(map[string]any), nil
}

func consumeUpdateErrorResult() (i18n.Result, error) {
	path, err := updateErrorPath()
	if err != nil {
		return i18n.Result{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := removeUpdateErrorMetadata(path); err != nil {
			return i18n.Result{}, err
		}
		return i18n.Result{Value: map[string]any{"error": ""}}, nil
	}
	if err != nil {
		return i18n.Result{}, err
	}
	message := output.RedactError(string(data))
	descriptor, valid := readUpdateErrorMessage(path, message)
	if err := removeUpdateErrorMetadata(path); err != nil {
		return i18n.Result{}, err
	}
	if err := os.Remove(path); err != nil {
		return i18n.Result{}, err
	}
	result := i18n.Result{Value: map[string]any{"error": message}}
	if valid && message != "" {
		result.Messages = map[string]i18n.Descriptor{"/error": descriptor}
	}
	return result, nil
}

// The shell keeps the inherited maintenance lock and authorization worker alive
// across App exit. Preparation has already verified the private release files.
func (m *Manager) launchBackgroundUpdate(ctx context.Context, r record, artifact string, running bool, lock *os.File) (map[string]any, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	logDir := filepath.Join(cache, "Nagi")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(filepath.Join(logDir, "update.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()
	bridge, err := os.MkdirTemp("", "nagi-update-authorization-")
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			os.RemoveAll(bridge)
		}
	}()
	for name, source := range map[string]string{"sudo": nativeUpdateSudo, "worker": nativeUpdateWorker} {
		if err := os.WriteFile(filepath.Join(bridge, name), []byte(source), 0700); err != nil {
			return nil, err
		}
	}
	ready := filepath.Join(bridge, "authorized")
	failed := filepath.Join(bridge, "authorization-failed")
	proceed := filepath.Join(bridge, "proceed")
	errorPath := filepath.Join(logDir, "update-error")
	fallback := i18n.Errorf("system.installation.update.failed", "Update failed. Inspect the Nagi update log and retry the update.")
	metadata, _ := json.Marshal(updateErrorMetadata{Digest: updateErrorDigest(fallback.Error()), Message: fallback.Descriptor()})
	// Only the owner shell and the update CLI may inherit the lock. In particular,
	// an authorization or polling child must not delay retry after owner exit.
	command := "umask 077; export NAGI_UPDATE_OWNER_PID=$$; trap " + shellQuote("/bin/rm -f -- "+shellQuote(filepath.Join(bridge, "alive"))+" 3>&-; /bin/rm -rf -- "+shellQuote(bridge)+" "+shellQuote(filepath.Dir(artifact))+" 3>&-") + " EXIT; " +
		"if [ " + shellQuote(r.Prefix) + " = /usr/local ] || [ ! -w " + shellQuote(filepath.Join(r.Prefix, "bin")) + " ] || [ ! -w " + shellQuote(filepath.Join(r.Prefix, "share", "nagi")) + " ] || [ ! -w " + shellQuote(filepath.Dir(r.App)) + " ]; then sudo -v < /dev/null 3>&- || { touch " + shellQuote(failed) + " 3>&-; exit 1; }; fi; " +
		"touch " + shellQuote(ready) + " 3>&-; i=0; while [ ! -e " + shellQuote(proceed) + " ]; do i=$((i+1)); [ \"$i\" -lt 100 ] || exit 1; sleep 0.1 3>&-; done; " +
		"exec 4>&-; i=0; while pgrep -x NagiApp >/dev/null 2>&1 3>&-; do i=$((i+1)); if [ \"$i\" -ge 60 ]; then echo " + shellQuote(m.Locale.Render(i18n.Errorf("system.installation.update.cancelled", "Update cancelled: Nagi.app did not quit."))) + "; exit 1; fi; sleep 1 3>&-; done; " +
		"NAGI_UPDATE_LOCK_FD=3 NAGI_UPDATE_ARTIFACT=" + shellQuote(artifact) + fmt.Sprintf(" NAGI_UPDATE_ENGINE_RUNNING=%t ", running) + shellQuote(m.Executable) + " --json update; result=$?; if [ \"$result\" -ne 0 ] && [ ! -e " + shellQuote(errorPath) + " ]; then printf '%s' " + shellQuote(fallback.Error()) + " > " + shellQuote(errorPath) + "; printf '%s' " + shellQuote(string(metadata)) + " > " + shellQuote(errorPath+".messages") + "; fi; /usr/bin/open " + shellQuote(r.App) + " 3>&-"
	// EOF on this inherited pipe joins the pre-handoff process tree, not just
	// its shell leader. SIGKILL delivery to the group is not an exit barrier.
	lifetime, lifetimeWriter, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer lifetime.Close()
	defer lifetimeWriter.Close()
	cmd := exec.Command("/bin/bash", "-c", command)
	cmd.Env = append(os.Environ(), "NAGI_UPDATE_PROMPT="+m.Locale.Render(i18n.Errorf("system.installation.update.permission", "Nagi needs permission to update.")), "PATH="+bridge+":"+os.Getenv("PATH"), fmt.Sprintf("NAGI_UPDATE_REQUEST_PID=%d", os.Getpid()))
	cmd.ExtraFiles = []*os.File{lock, lifetimeWriter}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	start := cmd.Start
	if m.StartBackground != nil {
		start = func() error { return m.StartBackground(cmd) }
	}
	if err := start(); err != nil {
		return nil, i18n.Errorf("system.installation.cannot.start.background.update", "cannot start background update: %w", err)
	}
	_ = lifetimeWriter.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	unconfirmed := func(cause, drainErr error) error {
		// Keep the typed drain message as the direct argument so its descriptor
		// survives composition; retain cancellation in the separate cause graph.
		var source *i18n.Message
		if errors.As(drainErr, &source) {
			descriptor := source.Descriptor()
			drainErr = i18n.NewMessage(descriptor.ID, descriptor.Args, source.Error(), errors.Join(cause, drainErr))
		}
		return i18n.Errorf("system.installation.cannot.start.background.update", "cannot start background update: %w", drainErr)
	}
	stop := func() error {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return drainBackgroundLifetime(lifetime, time.Now().Add(5*time.Second))
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := stop(); err != nil {
				return nil, unconfirmed(ctx.Err(), err)
			}
			return nil, i18n.Errorf("system.installation.update.preparation.cancelled.nagi.remains.running", "update preparation cancelled; Nagi remains running: %w", ctx.Err())
		case err := <-done:
			// The leader is already reaped; do not signal a potentially reused PGID.
			if drainErr := drainBackgroundLifetime(lifetime, time.Now().Add(5*time.Second)); drainErr != nil {
				return nil, unconfirmed(err, drainErr)
			}
			return nil, i18n.Errorf("system.installation.cannot.authorize.update.nagi.remains.running.retry.update.and.approve", "cannot authorize update; Nagi remains running; retry Update and approve the macOS authorization dialog: %v", err)
		case <-ticker.C:
			if _, err := os.Stat(failed); err == nil {
				if err := stop(); err != nil {
					return nil, unconfirmed(errors.New("update authorization failed"), err)
				}
				return nil, i18n.Errorf("system.installation.update.authorization.was.cancelled.or.failed.nagi.remains.running", "update authorization was cancelled or failed; Nagi remains running")
			}
			if _, err := os.Stat(ready); err == nil {
				// The acknowledgment prevents a child from deleting artifacts before its
				// parent has accepted ownership transfer and released its descriptor.
				if path, e := updateErrorPath(); e == nil {
					_ = os.Remove(path)
				}
				if err := os.WriteFile(proceed, []byte("ready"), 0600); err != nil {
					if drainErr := stop(); drainErr != nil {
						return nil, unconfirmed(err, drainErr)
					}
					return nil, err
				}
				cleanup = false
				return map[string]any{"action": "update", "scheduled": true, "background": true}, nil
			}
		}
	}
}

// drainBackgroundLifetime bounds confirmation when a descendant escapes the
// process group or retains the lifetime descriptor unexpectedly. Failure must
// not be reported as confirmed shutdown or maintenance-lock release.
func drainBackgroundLifetime(lifetime *os.File, deadline time.Time) error {
	if err := lifetime.SetReadDeadline(deadline); err != nil {
		return i18n.Errorf("system.installation.background.shutdown.unconfirmed", "background descendant shutdown unconfirmed; maintenance lock release unconfirmed: %w", err)
	}
	if _, err := io.Copy(io.Discard, lifetime); err != nil {
		return i18n.Errorf("system.installation.background.shutdown.unconfirmed", "background descendant shutdown unconfirmed; maintenance lock release unconfirmed: %w", err)
	}
	return nil
}

// Ensure the artifact passed across the private handoff has the expected shape.
func validPreparedArtifact(path string) bool {
	return filepath.IsAbs(path) && filepath.Base(path) == "artifact" && strings.HasPrefix(filepath.Base(filepath.Dir(path)), "nagi-release-")
}
