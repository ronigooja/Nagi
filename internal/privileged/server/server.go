// Package server contains guarded primitives for the privileged helper.
// It does not install, launch, or expose a root mihomo process.
package server

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/privileged"

	"github.com/ronigooja/Nagi/internal/i18n"
)

// Handler receives only validated requests from the fixed session user.
// The implementation must still safely open paths and validate root mihomo
// configuration; request validation alone cannot prevent filesystem races.
type Handler interface {
	Handle(context.Context, int, privileged.Request) (privileged.Response, error)
}

type Server struct {
	SessionUID int
	Policy     privileged.Policy
	Handler    Handler
}

const maxHelperConnections = 32

// Serve accepts connections on a pre-created, fixed-session socket. The
// caller must create this socket inside a root-controlled directory and set
// its ownership before calling Serve. The handler owns operation serialization;
// at most maxHelperConnections requests are served concurrently. Serve cancels
// and drains all accepted connections before returning.

func (s Server) Serve(ctx context.Context, listener *net.UnixListener) error {
	if listener == nil {
		return i18n.Errorf("system.privileged.nil.helper.listener", "nil helper listener")
	}
	addr, ok := listener.Addr().(*net.UnixAddr)
	if !ok || addr.Name == "" {
		return i18n.Errorf("system.privileged.helper.listener.has.no.filesystem.path", "helper listener has no filesystem path")
	}
	if err := VerifySessionSocket(addr.Name, s.SessionUID); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	stopListener := closeOnCancellation(ctx, func() { _ = listener.Close() })
	var workers sync.WaitGroup
	defer func() {
		cancel()
		_ = listener.Close()
		stopListener()
		workers.Wait()
	}()
	slots := make(chan struct{}, maxHelperConnections)
	for {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return nil
		}
		conn, err := listener.AcceptUnix()
		if err != nil {
			<-slots
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { <-slots }()
			// An invalid request affects only its connection.
			_ = s.ServeConn(ctx, conn)
		}()
	}
}

// ServeConn handles one request from a Unix socket client. The UID supplied
// in Policy is ignored: only the kernel-reported peer credential is trusted.
func (s Server) ServeConn(ctx context.Context, conn *net.UnixConn) error {
	defer conn.Close()
	stop := closeOnCancellation(ctx, func() { _ = conn.Close() })
	defer stop()
	if s.SessionUID <= 0 || s.Handler == nil {
		return i18n.Errorf("system.privileged.helper.session.is.not.configured", "helper session is not configured")
	}
	uid, err := peerUID(conn)
	if err != nil {
		return i18n.Errorf("system.privileged.read.peer.credential", "read peer credential: %w", err)
	}
	if uid != s.SessionUID {
		return i18n.Errorf("system.privileged.peer.uid.is.not.the.bound.session.user", "peer uid %d is not the bound session user", uid)
	}
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	req, err := privileged.ReadRequest(conn)
	if err != nil {
		return i18n.Errorf("system.privileged.read.request", "read request: %w", err)
	}
	policy := s.Policy
	policy.PeerUID = uid
	if err := req.Validate(policy); err != nil {
		return i18n.Errorf("system.privileged.validate.request", "validate request: %w", err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return err
	}
	resp, err := s.Handler.Handle(ctx, uid, req)
	if err != nil {
		resp = helperFailureResponse(req, resp, err)
	}
	resp.Version = privileged.ProtocolVersion
	if err := resp.Validate(); err != nil {
		return i18n.Errorf("system.privileged.invalid.handler.response", "invalid handler response: %w", err)
	}
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return privileged.WriteResponse(conn, resp)
}

// helperFailureResponse preserves only evidence supplied by the reload handler.
func helperFailureResponse(req privileged.Request, resp privileged.Response, err error) privileged.Response {
	// Preserve pre-forward evidence only for the fixed configuration reload.
	notForwarded := resp.NotForwarded && req.Operation == privileged.Control && req.Method == "PUT" && req.Path == "/configs?force=true"
	resp = privileged.Response{Version: privileged.ProtocolVersion, Error: err.Error(), NotForwarded: notForwarded}
	var message *i18n.Message
	if errors.As(err, &message) && message.Error() == err.Error() {
		resp.Messages = map[string]i18n.Descriptor{"/error": message.Descriptor()}
	}
	if errors.Is(err, control.ErrResponseTooLarge) {
		resp.Messages = nil
		resp.Error = "response_too_large"
	}
	return resp
}

// VerifySessionSocket confirms that an already-bound listener has not been
// replaced and is owned only by the configured user. The socket directory
// must remain root controlled; a listener should be created there by root,
// then chowned to the one session UID and chmodded to 0600.
func VerifySessionSocket(path string, uid int) error {
	if uid <= 0 {
		return i18n.Errorf("system.privileged.invalid.session.uid", "invalid session uid")
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return i18n.Errorf("system.privileged.socket.path.must.be.absolute.and.clean", "socket path must be absolute and clean")
	}
	if err := verifyOwned(path, uint32(uid), os.ModeSocket, 0600); err != nil {
		return err
	}
	return verifyAncestors(filepath.Dir(path), 0)
}

// VerifyControllerSocket requires an isolated root-owned controller socket.
// Do not chown this socket to the session user or expose its raw HTTP API.
func VerifyControllerSocket(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return i18n.Errorf("system.privileged.controller.path.must.be.absolute.and.clean", "controller path must be absolute and clean")
	}
	if err := verifyOwned(path, 0, os.ModeSocket, 0600); err != nil {
		return err
	}
	return verifyAncestors(filepath.Dir(path), 0)
}

func verifyAncestors(path string, uid uint32) error {
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || stat.Uid != uid || info.Mode().Perm()&0022 != 0 {
			return i18n.Errorf("system.privileged.unsafe.directory", "unsafe directory %s", path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func verifyOwned(path string, uid uint32, kind os.FileMode, perm os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeType != kind || stat.Uid != uid || info.Mode().Perm() != perm {
		return i18n.Errorf("system.privileged.unsafe.socket", "unsafe socket %s", path)
	}
	return nil
}

// closeOnCancellation unblocks socket reads and writes without requiring a
// client deadline. The returned function also waits for its watcher to exit.
func closeOnCancellation(ctx context.Context, closeSocket func()) func() {
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		select {
		case <-ctx.Done():
			closeSocket()
		case <-done:
		}
	}()
	return func() { close(done); <-exited }
}
