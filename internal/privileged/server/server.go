// Package server contains guarded primitives for the privileged helper.
// It does not install, launch, or expose a root mihomo process.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ronigooja/Nagi/internal/privileged"
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

// Serve accepts connections on a pre-created, fixed-session socket. The
// caller must create this socket inside a root-controlled directory and set
// its ownership before calling Serve. Requests are serialized so lifecycle
// operations cannot race each other inside this helper.
func (s Server) Serve(ctx context.Context, listener *net.UnixListener) error {
	if listener == nil {
		return errors.New("nil helper listener")
	}
	addr, ok := listener.Addr().(*net.UnixAddr)
	if !ok || addr.Name == "" {
		return errors.New("helper listener has no filesystem path")
	}
	if err := VerifySessionSocket(addr.Name, s.SessionUID); err != nil {
		return err
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			listener.Close()
		case <-done:
		}
	}()
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		// An invalid request affects only its connection. The next client may
		// still recover the session without restarting the root service.
		_ = s.ServeConn(ctx, conn)
	}
}

// ServeConn handles one request from a Unix socket client. The UID supplied
// in Policy is ignored: only the kernel-reported peer credential is trusted.
func (s Server) ServeConn(ctx context.Context, conn *net.UnixConn) error {
	defer conn.Close()
	if s.SessionUID <= 0 || s.Handler == nil {
		return errors.New("helper session is not configured")
	}
	uid, err := peerUID(conn)
	if err != nil {
		return fmt.Errorf("read peer credential: %w", err)
	}
	if uid != s.SessionUID {
		return fmt.Errorf("peer uid %d is not the bound session user", uid)
	}
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	req, err := privileged.ReadRequest(conn)
	if err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	policy := s.Policy
	policy.PeerUID = uid
	if err := req.Validate(policy); err != nil {
		return fmt.Errorf("validate request: %w", err)
	}
	resp, err := s.Handler.Handle(ctx, uid, req)
	if err != nil {
		resp = privileged.Response{Version: privileged.ProtocolVersion, Error: err.Error()}
	}
	resp.Version = privileged.ProtocolVersion
	if err := resp.Validate(); err != nil {
		return fmt.Errorf("invalid handler response: %w", err)
	}
	return privileged.WriteResponse(conn, resp)
}

// VerifySessionSocket confirms that an already-bound listener has not been
// replaced and is owned only by the configured user. The socket directory
// must remain root controlled; a listener should be created there by root,
// then chowned to the one session UID and chmodded to 0600.
func VerifySessionSocket(path string, uid int) error {
	if uid <= 0 {
		return errors.New("invalid session uid")
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("socket path must be absolute and clean")
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
		return errors.New("controller path must be absolute and clean")
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
			return fmt.Errorf("unsafe directory %s", path)
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
		return fmt.Errorf("unsafe socket %s", path)
	}
	return nil
}
