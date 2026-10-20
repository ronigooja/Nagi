package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/ronigooja/Nagi/internal/profile"
)

func writeProxyNode(w io.Writer, node profile.ProxyNode) error {
	if err := line(w, renderText(w, "cli.output.proxy_node.summary", map[string]any{"name": node.Name, "type": node.Type, "managed": node.Managed}, fmt.Sprintf("%s (%s); managed: %t", node.Name, node.Type, node.Managed))); err != nil {
		return err
	}
	if node.Tailscale != nil {
		n := node.Tailscale
		if err := line(w, renderText(w, "cli.output.proxy_node.tailscale", map[string]any{"exit": n.ExitNode, "hostname": n.Hostname, "control_url": n.ControlURL, "auth_key_configured": n.AuthKeyConfigured}, fmt.Sprintf("Exit node: %s; hostname: %s; control URL: %s; auth key configured: %t", n.ExitNode, n.Hostname, n.ControlURL, n.AuthKeyConfigured))); err != nil {
			return err
		}
	}
	return writeProxyNodeGroups(w, node.Groups)
}
func writeProxyNodeGroups(w io.Writer, g profile.ProxyNodeGroups) error {
	for _, entry := range []struct {
		id, label string
		names     []string
	}{{"explicit", "Explicit groups", g.Explicit}, {"automatic", "Automatic groups", g.Automatic}, {"excluded", "Excluded automatic groups", g.Excluded}, {"available", "Available groups", g.Available}} {
		names := strings.Join(entry.names, ", ")
		if names == "" {
			names = text(w, "cli.output.proxy_node.none", "none")
		}
		if err := line(w, renderText(w, "cli.output.proxy_node.groups."+entry.id, map[string]any{"groups": names}, entry.label+": "+names)); err != nil {
			return err
		}
	}
	return nil
}
func writeProxyChainTarget(w io.Writer, t profile.ProxyChainTarget) error {
	path := append(append([]string{}, t.Via...), t.Name)
	return line(w, renderText(w, "cli.output.proxy_chain.target", map[string]any{"kind": t.Kind, "name": t.Name, "path": strings.Join(path, " -> ")}, fmt.Sprintf("%s %s: %s", t.Kind, t.Name, strings.Join(path, " -> "))))
}
func writeProxyChainPlan(w io.Writer, p profile.ProxyChainPlan, dry bool) error {
	if dry {
		if err := line(w, text(w, "cli.output.proxy_chain.preview", "Dry run; no configuration saved or activated.")); err != nil {
			return err
		}
	}
	for _, target := range p.Targets {
		if err := writeProxyChainTarget(w, profile.ProxyChainTarget{Kind: p.Kind, Name: target, Via: p.Via}); err != nil {
			return err
		}
	}
	for _, change := range p.Changes {
		before, after := change.Before, change.After
		if before == "" {
			before = text(w, "cli.output.proxy_node.none", "none")
		}
		if after == "" {
			after = text(w, "cli.output.proxy_node.none", "none")
		}
		if err := line(w, renderText(w, "cli.output.proxy_chain.change", map[string]any{"kind": change.Kind, "name": change.Name, "before": before, "after": after}, fmt.Sprintf("Replace %s %s: %s -> %s", change.Kind, change.Name, before, after))); err != nil {
			return err
		}
	}
	for _, ref := range p.SharedUse {
		if err := line(w, renderText(w, "cli.output.proxy_chain.shared", map[string]any{"kind": ref.Kind, "name": ref.Name, "via": ref.Via}, fmt.Sprintf("Shared impact: %s %s via %s", ref.Kind, ref.Name, ref.Via))); err != nil {
			return err
		}
	}
	for index, conflict := range p.Conflicts {
		conflict = RenderMessageField(p, fmt.Sprintf("/conflicts/%d", index), conflict, writerLanguage(w))
		if err := line(w, renderText(w, "cli.output.proxy_chain.conflict", map[string]any{"conflict": conflict}, "Conflict: "+conflict)); err != nil {
			return err
		}
	}
	return nil
}
