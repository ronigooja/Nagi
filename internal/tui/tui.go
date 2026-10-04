// Package tui implements Nagi's terminal dashboard. It contains no runtime or
// control API access; the command package supplies those operations.
package tui

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode"
)

type Group struct {
	Name, Now string
	Nodes     []string
}
type Snapshot struct {
	Running                              bool
	Profile, Mode, DNS, SystemProxy, TUN string
	Groups                               []Group
	Subscriptions                        []string
	Logs                                 []string
}
type Backend interface {
	Load() (Snapshot, error)
	Do(command string, args ...string) (string, error)
}
type App struct {
	In                        io.Reader
	Out                       io.Writer
	Backend                   Backend
	State                     Snapshot
	Filter                    string
	Group, Node, Subscription int
	Message                   string
	Error                     string
	Loading                   bool
	ShowLogs                  bool
}

func FilterNodes(nodes []string, query string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if strings.Contains(strings.ToLower(n), q) {
			out = append(out, n)
		}
	}
	return out
}
func clean(v string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, v)
}
func (a *App) group() *Group {
	if a.Group < 0 || a.Group >= len(a.State.Groups) {
		return nil
	}
	return &a.State.Groups[a.Group]
}
func (a *App) nodes() []string {
	g := a.group()
	if g == nil {
		return nil
	}
	return FilterNodes(g.Nodes, a.Filter)
}
func (a *App) selectedNode() string {
	n := a.nodes()
	if a.Node < 0 || a.Node >= len(n) {
		return ""
	}
	return n[a.Node]
}
func (a *App) Render() error {
	if _, err := io.WriteString(a.Out, "\033[H\033[2J"); err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "Nagi TUI  q quit  r refresh  j/k node  n/N group  / search  c clear")
	fmt.Fprintln(a.Out, "s select  d delay  m mode  u update  a apply  l logs  p system proxy  t TUN")
	state := "stopped"
	if a.State.Running {
		state = "running"
	}
	fmt.Fprintf(a.Out, "Engine: %s  Profile: %s  Mode: %s  DNS: %s\n", state, display(a.State.Profile), display(a.State.Mode), display(a.State.DNS))
	fmt.Fprintf(a.Out, "System proxy: %s  TUN: %s\n", display(a.State.SystemProxy), display(a.State.TUN))
	if a.Loading {
		fmt.Fprintln(a.Out, "\nLoading…")
		return nil
	}
	if a.Error != "" {
		fmt.Fprintf(a.Out, "\nError: %s\n", clean(a.Error))
	}
	if a.Message != "" {
		fmt.Fprintf(a.Out, "\n%s\n", clean(a.Message))
	}
	if a.ShowLogs {
		fmt.Fprintln(a.Out, "\nRecent logs:")
		if len(a.State.Logs) == 0 {
			fmt.Fprintln(a.Out, "No logs available.")
		}
		for _, line := range a.State.Logs {
			fmt.Fprintln(a.Out, clean(line))
		}
		return nil
	}
	fmt.Fprintln(a.Out, "\nProxy groups:")
	if len(a.State.Groups) == 0 {
		fmt.Fprintln(a.Out, "No proxy groups available. Start mihomo or check the control API.")
	}
	for i, g := range a.State.Groups {
		mark := " "
		if i == a.Group {
			mark = ">"
		}
		fmt.Fprintf(a.Out, " %s %s (current: %s)\n", mark, clean(g.Name), display(g.Now))
	}
	if g := a.group(); g != nil {
		fmt.Fprintf(a.Out, "\nNodes in %s", clean(g.Name))
		if a.Filter != "" {
			fmt.Fprintf(a.Out, " (search: %s)", clean(a.Filter))
		}
		fmt.Fprintln(a.Out, ":")
		nodes := a.nodes()
		if len(nodes) == 0 {
			fmt.Fprintln(a.Out, "No nodes match.")
		}
		for i, node := range nodes {
			mark := " "
			if i == a.Node {
				mark = ">"
			}
			current := ""
			if node == g.Now {
				current = " *"
			}
			fmt.Fprintf(a.Out, " %s %s%s\n", mark, clean(node), current)
		}
	}
	fmt.Fprintln(a.Out, "\nSubscriptions:")
	if len(a.State.Subscriptions) == 0 {
		fmt.Fprintln(a.Out, "No saved subscriptions.")
	}
	for i, name := range a.State.Subscriptions {
		mark := " "
		if i == a.Subscription {
			mark = ">"
		}
		fmt.Fprintf(a.Out, " %s %s\n", mark, clean(name))
	}
	fmt.Fprintln(a.Out, "[ and ] choose subscription")
	return nil
}
func display(v string) string {
	if v == "" {
		return "unknown"
	}
	return clean(v)
}
func (a *App) refresh() {
	a.Loading = true
	_ = a.Render()
	s, err := a.Backend.Load()
	a.Loading = false
	if err != nil {
		a.Error = err.Error()
		return
	}
	a.State = s
	a.Error = ""
	if a.Group >= len(s.Groups) {
		a.Group = 0
	}
	if a.Subscription >= len(s.Subscriptions) {
		a.Subscription = 0
	}
	if a.Node >= len(a.nodes()) {
		a.Node = 0
	}
}
func (a *App) action(command string, args ...string) {
	a.Loading = true
	_ = a.Render()
	msg, err := a.Backend.Do(command, args...)
	a.Loading = false
	if err != nil {
		a.Error = err.Error()
		a.Message = ""
		return
	}
	a.Message = msg
	a.Error = ""
	a.refresh()
}
func (a *App) prompt(in *bufio.Reader, label string) (string, error) {
	fmt.Fprintf(a.Out, "\n%s: ", label)
	var b strings.Builder
	for {
		r, _, err := in.ReadRune()
		if err != nil {
			return "", err
		}
		if r == '\r' || r == '\n' {
			fmt.Fprintln(a.Out)
			return strings.TrimSpace(b.String()), nil
		}
		if r == 127 || r == '\b' {
			s := []rune(b.String())
			if len(s) > 0 {
				b.Reset()
				b.WriteString(string(s[:len(s)-1]))
				io.WriteString(a.Out, "\b \b")
			}
			continue
		}
		if unicode.IsPrint(r) && b.Len() < 128 {
			b.WriteRune(r)
			io.WriteString(a.Out, string(r))
		}
	}
}
func (a *App) Handle(in *bufio.Reader, k rune) bool {
	switch k {
	case 'q', 'Q':
		return false
	case 'r', 'R':
		a.refresh()
	case 'j':
		if a.Node+1 < len(a.nodes()) {
			a.Node++
		}
	case 'k':
		if a.Node > 0 {
			a.Node--
		}
	case 'n':
		if a.Group+1 < len(a.State.Groups) {
			a.Group++
			a.Node = 0
		}
	case 'N':
		if a.Group > 0 {
			a.Group--
			a.Node = 0
		}
	case '[':
		if a.Subscription > 0 {
			a.Subscription--
		}
	case ']':
		if a.Subscription+1 < len(a.State.Subscriptions) {
			a.Subscription++
		}
	case '/':
		q, err := a.prompt(in, "Search nodes")
		if err != nil {
			a.Error = err.Error()
		} else {
			a.Filter = q
			a.Node = 0
		}
	case 'c':
		a.Filter = ""
		a.Node = 0
	case 's', 'd':
		g := a.group()
		node := a.selectedNode()
		if g == nil || node == "" {
			a.Error = "Choose a node first."
		} else if k == 's' {
			a.action("proxy", "select", g.Name, node)
		} else {
			a.action("proxy", "delay", node)
		}
	case 'm':
		modes := []string{"rule", "global", "direct"}
		next := modes[0]
		for i, v := range modes {
			if v == a.State.Mode {
				next = modes[(i+1)%len(modes)]
				break
			}
		}
		a.action("mode", next)
	case 'u', 'a':
		if a.Subscription >= len(a.State.Subscriptions) {
			a.Error = "No subscription selected."
		} else {
			verb := "update"
			if k == 'a' {
				verb = "apply"
			}
			a.action("subscription", verb, a.State.Subscriptions[a.Subscription])
		}
	case 'l':
		a.ShowLogs = !a.ShowLogs
	case 'p', 't':
		target := "system-proxy"
		state := a.State.SystemProxy
		if k == 't' {
			target = "tun"
			state = a.State.TUN
		}
		verb := "enable"
		if state == "enabled" {
			verb = "disable"
		}
		a.action(target, verb)
	}
	return true
}
func (a *App) Run() error {
	if a.In == nil || a.Out == nil || a.Backend == nil {
		return fmt.Errorf("TUI requires input, output, and backend")
	}
	a.refresh()
	if err := a.Render(); err != nil {
		return err
	}
	in := bufio.NewReader(a.In)
	for {
		r, _, err := in.ReadRune()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if !a.Handle(in, r) {
			_, err = io.WriteString(a.Out, "\n")
			return err
		}
		if err := a.Render(); err != nil {
			return err
		}
	}
}
