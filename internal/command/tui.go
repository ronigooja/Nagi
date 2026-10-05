package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/ronigooja/Nagi/internal/proxy"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"github.com/ronigooja/Nagi/internal/subscription"
	"github.com/ronigooja/Nagi/internal/tui"
)

type tuiBackend struct{ version, commit string }

func (b tuiBackend) run(args ...string) (any, error) {
	return execute(context.Background(), args, b.version, b.commit)
}
func (b tuiBackend) Load() (tui.Snapshot, error) {
	var s tui.Snapshot
	status, err := b.run("status")
	if err != nil {
		return s, err
	}
	data := status.(map[string]any)
	s.Running, _ = data["running"].(bool)
	s.Profile, _ = data["profile"].(string)
	if message, ok := data["profile_error"].(string); ok {
		s.Profile = "unavailable: " + message
	}
	s.Mode = "unavailable"
	s.DNS = "unavailable"
	s.SystemProxy = "unavailable"
	s.TUN = "unavailable"
	if s.Running {
		if result, err := b.run("mode"); err == nil {
			s.Mode, _ = result.(map[string]any)["mode"].(string)
		}
		if result, err := b.run("proxy", "groups"); err == nil {
			for _, g := range result.(map[string]any)["groups"].([]proxy.Group) {
				s.Groups = append(s.Groups, tui.Group{Name: g.Name, Now: g.Now, Nodes: g.All})
			}
		}
		if paths, err := nagiruntime.Resolve(); err == nil {
			var cfg map[string]any
			client, clientErr := controlClientForCurrentBackend(paths)
			if clientErr == nil && client.Get(context.Background(), "/configs", &cfg) == nil {
				if dns, ok := cfg["dns"].(map[string]any); ok {
					if enabled, ok := dns["enable"].(bool); ok {
						if enabled {
							s.DNS = "enabled"
						} else {
							s.DNS = "disabled"
						}
					}
				}
				if tun, ok := cfg["tun"].(map[string]any); ok {
					if enabled, ok := tun["enable"].(bool); ok {
						if enabled {
							s.TUN = "enabled"
						} else {
							s.TUN = "disabled"
						}
					}
				}
			}
		}
	}
	if result, err := b.run("subscription", "list"); err == nil {
		for _, entry := range result.(map[string]any)["subscriptions"].([]subscription.Entry) {
			s.Subscriptions = append(s.Subscriptions, entry.Name)
		}
	}
	if result, err := b.run("logs", "20"); err == nil {
		if lines, ok := result.(map[string]any)["lines"].([]string); ok {
			s.Logs = lines
		}
	}
	for _, target := range []string{"system-proxy", "tun"} {
		if result, err := b.run(target, "status"); err == nil {
			if m, ok := result.(map[string]any); ok {
				state := "unknown"
				if enabled, ok := m["enabled"].(bool); ok {
					if enabled {
						state = "enabled"
					} else {
						state = "disabled"
					}
				}
				if target == "system-proxy" {
					s.SystemProxy = state
				} else {
					s.TUN = state
				}
			}
		}
	}
	return s, nil
}
func (b tuiBackend) Do(command string, args ...string) (string, error) {
	argv := append([]string{command}, args...)
	result, err := b.run(argv...)
	if err != nil {
		return "", err
	}
	switch command {
	case "proxy":
		if args[0] == "delay" {
			if d, ok := result.(proxy.DelayResult); ok {
				return fmt.Sprintf("%s: %d ms", d.Proxy, d.DelayMS), nil
			}
		}
		return "Node selected.", nil
	case "mode":
		return "Runtime mode changed.", nil
	case "subscription":
		if args[0] == "update" {
			return "Subscription downloaded; use apply to activate it.", nil
		}
		return "Subscription applied.", nil
	case "system-proxy", "tun":
		return command + " changed.", nil
	}
	return "Done.", nil
}
func runTUI(out, errOut io.Writer, version, commit string) int {
	inputInfo, err := os.Stdin.Stat()
	if err != nil || inputInfo.Mode()&os.ModeCharDevice == 0 {
		fmt.Fprintln(errOut, "TUI requires an interactive terminal on stdin")
		return 2
	}
	outputFile, ok := out.(*os.File)
	if !ok {
		fmt.Fprintln(errOut, "TUI requires an interactive terminal on stdout")
		return 2
	}
	outputInfo, err := outputFile.Stat()
	if err != nil || outputInfo.Mode()&os.ModeCharDevice == 0 {
		fmt.Fprintln(errOut, "TUI requires an interactive terminal on stdout")
		return 2
	}
	readTerminal := exec.Command("stty", "-g")
	readTerminal.Stdin = os.Stdin
	original, err := readTerminal.Output()
	if err != nil {
		fmt.Fprintln(errOut, "TUI cannot read terminal settings:", err)
		return 1
	}
	restore := strings.TrimSpace(string(original))
	defer func() {
		restoreTerminal := exec.Command("stty", restore)
		restoreTerminal.Stdin = os.Stdin
		_ = restoreTerminal.Run()
	}()
	cmd := exec.Command("stty", "raw", "-echo")
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(errOut, "TUI cannot enable terminal keys:", err)
		return 1
	}
	defer io.WriteString(out, "\033[0m\n")
	app := &tui.App{In: os.Stdin, Out: out, Backend: tuiBackend{version: version, commit: commit}}
	if err := app.Run(); err != nil {
		fmt.Fprintln(errOut, "TUI failed:", err)
		return 1
	}
	return 0
}
