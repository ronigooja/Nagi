package server

import (
	"context"
	"encoding/json"
	"net"
	"runtime"
	"time"

	"github.com/ronigooja/Nagi/internal/privileged"
	"github.com/ronigooja/Nagi/internal/traffic"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func (h *RuntimeHandler) dnsManager() *traffic.DNSManager { return traffic.NewDNS(h.PrivateDir) }
func (h *RuntimeHandler) handleDNS(ctx context.Context, op privileged.Operation) (privileged.Response, error) {
	if h.orphaned {
		return privileged.Response{}, i18n.Errorf("system.privileged.orphaned.privileged.engine.administrator.recovery.is.required.before.changing.tun", "orphaned_privileged_engine: administrator recovery is required before changing TUN DNS")
	}
	var state traffic.DNSState
	var err error
	switch op {
	case privileged.TUNDNSSync:
		state, err = h.syncDNS(ctx)
	case privileged.TUNDNSRestore:
		state, err = h.dnsManager().Disable(ctx)
	case privileged.TUNDNSStatus:
		state, err = h.dnsManager().Status(ctx)
	default:
		return privileged.Response{}, i18n.Errorf("system.privileged.unsupported.dns.operation", "unsupported DNS operation")
	}
	if op != privileged.TUNDNSStatus {
		h.dnsError = err
	}
	if err != nil {
		return privileged.Response{}, err
	}
	if op == privileged.TUNDNSStatus && h.dnsError != nil {
		return privileged.Response{}, h.dnsError
	}
	body, err := json.Marshal(state)
	return privileged.Response{OK: err == nil, Body: body}, err
}
func (h *RuntimeHandler) syncDNS(ctx context.Context) (traffic.DNSState, error) {
	m := h.dnsManager()
	if runtime.GOOS != "darwin" {
		return m.Status(ctx)
	}
	if h.child == nil {
		return m.Disable(ctx)
	}
	response, err := controlRequest(ctx, h.controllerPath(), privileged.Request{Method: "GET", Path: "/configs"})
	if err != nil {
		return traffic.DNSState{}, err
	}
	if !response.OK {
		return traffic.DNSState{}, i18n.Errorf("system.privileged.read.running.tun.configuration.http", "read running TUN configuration: HTTP %d", response.Status)
	}
	var config map[string]any
	if err = json.Unmarshal(response.Body, &config); err != nil {
		return traffic.DNSState{}, err
	}
	tun, _ := config["tun"].(map[string]any)
	if enabled, _ := tun["enable"].(bool); !enabled {
		return m.Disable(ctx)
	}
	dns, _ := config["dns"].(map[string]any)
	if enabled, _ := dns["enable"].(bool); !enabled {
		return h.unavailableDNS(ctx, i18n.Errorf("system.privileged.tun.dns.requires.running.mihomo.dns.enabled", "TUN DNS requires running mihomo DNS enabled"))
	}
	if route, _ := tun["auto-route"].(bool); !route {
		return h.unavailableDNS(ctx, i18n.Errorf("system.privileged.tun.dns.requires.auto.route", "TUN DNS requires auto-route"))
	}
	device, _ := tun["device"].(string)
	if err := waitTUNAdapter(ctx, device, net.InterfaceByName); err != nil {
		return h.unavailableDNS(ctx, err)
	}
	// Mihomo automatically intercepts port 53 on the derived TUN DNS endpoints.
	targets, err := traffic.DNSTargets(tun)
	if err != nil {
		return h.unavailableDNS(ctx, err)
	}
	return m.Sync(ctx, h.child.Process.Pid, targets)
}

// Adapter creation can lag behind mihomo's configured enable flag.
func waitTUNAdapter(ctx context.Context, device string, lookup func(string) (*net.Interface, error)) error {
	if device == "" {
		return i18n.Errorf("system.privileged.running.tun.has.no.adapter.name", "running TUN has no adapter name")
	}
	readyCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		adapter, err := lookup(device)
		if err == nil && adapter != nil && adapter.Flags&net.FlagUp != 0 {
			return nil
		}
		select {
		case <-readyCtx.Done():
			return i18n.Errorf("system.privileged.tun.adapter.is.not.ready", "TUN adapter %q is not ready: %w", device, readyCtx.Err())
		case <-ticker.C:
		}
	}
}

// Restore the baseline before reporting an unusable TUN resolver so an
// activation that completes after a client timeout does not strand OS DNS.
func (h *RuntimeHandler) unavailableDNS(ctx context.Context, cause error) (traffic.DNSState, error) {
	state, restoreErr := h.dnsManager().Disable(ctx)
	if restoreErr != nil {
		return state, i18n.Errorf("system.privileged.restore.system.dns", "%w; restore system DNS: %v", cause, restoreErr)
	}
	return state, cause
}

// Reconcile actual engine configuration so a delayed reload also updates DNS.
// This does not consume the lifecycle completion channel.
func (h *RuntimeHandler) watchDNS(pid int) {
	if runtime.GOOS != "darwin" {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		h.mu.Lock()
		if h.child == nil || h.child.Process.Pid != pid {
			h.mu.Unlock()
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, err := h.syncDNS(ctx)
		cancel()
		h.dnsError = err
		h.mu.Unlock()
	}
}
