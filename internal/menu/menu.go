package menu

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/ronigooja/Nagi/internal/output"
)

type Session struct {
	runner     Runner
	in         *bufio.Reader
	out        io.Writer
	file       *os.File
	interrupts chan os.Signal
}

func NewSession(runner Runner, in io.Reader, out io.Writer) *Session {
	s := &Session{runner: runner, in: bufio.NewReader(in), out: out}
	s.file, _ = in.(*os.File)
	return s
}

func (s *Session) Fetch(ctx context.Context, args ...string) Result {
	return s.runner.Run(ctx, args)
}

func (s *Session) Run(ctx context.Context, args ...string) Result {
	s.Printf("Executing: %s\n", output.RedactError(strings.Join(args, " ")))
	result := s.Fetch(ctx, args...)
	s.ShowResult(args, result)
	s.discardInterrupts()
	return result
}

func (s *Session) Stream(ctx context.Context, args ...string) Result {
	streamer, ok := s.runner.(StreamRunner)
	if !ok {
		return s.Run(ctx, args...)
	}
	streamCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	defer s.discardInterrupts()
	result := streamer.Stream(streamCtx, args, s.out)
	if streamCtx.Err() != nil && ctx.Err() == nil {
		return Result{}
	}
	if result.Error != nil || result.Code != 0 {
		s.showResult(result)
	}
	return result
}

var registered = map[string]func() *Page{}

// RegisterPage connects a feature page to the root menu without coupling the
// core to a feature's implementation file.
func RegisterPage(key string, factory func() *Page) { registered[key] = factory }

type rootEntry struct{ key, label string }

var rootEntries = []rootEntry{
	{"runtime", "Runtime status"},
	{"proxy", "Proxies and groups"},
	{"profile", "Profiles and subscriptions"},
	{"dns", "DNS"},
	{"rules", "Rules"},
	{"traffic", "Traffic access"},
	{"connections", "Connections"},
	{"logs", "Logs and traffic"},
	{"service", "Service and startup"},
	{"diagnostics", "Diagnostics and recovery"},
}

func RootPage() *Page {
	return &Page{Title: "Nagi menu", Help: []string{"menu"}, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: statusSummary(s.Fetch(ctx, "status"))}
		for _, entry := range rootEntries {
			entry := entry
			factory := registered[entry.key]
			if factory == nil {
				continue
			}
			view.Items = append(view.Items, Item{Label: entry.label, Next: factory})
		}
		return view, nil
	}}
}

type frame struct {
	page   *Page
	view   PageView
	index  int
	filter string
	loaded bool
	render bool
}

func (f *frame) resources() []Item {
	if f.filter == "" {
		return f.view.Items
	}
	items := []Item{}
	for _, item := range f.view.Items {
		name := item.Name
		if name == "" {
			name = item.Label
		}
		if strings.Contains(strings.ToLower(name), strings.ToLower(f.filter)) {
			items = append(items, item)
		}
	}
	return items
}

// Run drives the scrollback-preserving menu; operations remain CLI commands.
func Run(ctx context.Context, s *Session) int { return RunPage(ctx, s, RootPage()) }

func RunPage(ctx context.Context, s *Session, root *Page) int {
	s.interrupts = make(chan os.Signal, 8)
	signal.Notify(s.interrupts, os.Interrupt)
	defer signal.Stop(s.interrupts)
	stack := []frame{{page: root, render: true}}
	for len(stack) > 0 {
		current := &stack[len(stack)-1]
		if !current.loaded {
			if current.page.Load != nil {
				view, err := current.page.Load(ctx, s)
				if err != nil {
					s.Printf("Error refreshing %s: %s\n", current.page.Title, output.RedactError(err.Error()))
					view.Items = nil // Failed reads cannot leave stale resources selectable.
					if view.Empty == "" {
						view.Empty = "Data unavailable. Refresh or go back."
					}
				}
				current.view = view
			}
			current.loaded = true
			last := (len(current.resources()) - 1) / pageSize
			if current.index > last {
				current.index = last
			}
			if current.index < 0 {
				current.index = 0
			}
			current.render = true
		}
		resources := current.resources()
		if current.render {
			view := current.view
			view.Items = resources
			if current.filter != "" {
				view.Context += "\nFilter: " + current.filter
			}
			s.render(current.page, view, current.index, len(stack))
			current.render = false
		}
		value, err := s.readLine("Choice: ")
		if errors.Is(err, ErrQuit) || errors.Is(err, context.Canceled) {
			return 0
		}
		if errors.Is(err, ErrBack) {
			value = "b"
			if len(stack) == 1 {
				return 0
			}
		} else if err != nil {
			s.Printf("Input error: %s\n", err)
			return 1
		}
		if strings.HasPrefix(value, "/") {
			current.filter = strings.TrimSpace(value[1:])
			current.index = 0
			current.render = true
			continue
		}
		value = strings.ToLower(value)
		switch value {
		case "":
			continue
		case "q":
			return 0
		case "b":
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
				stack[len(stack)-1].loaded = false
			} else {
				s.WriteLine("Already at the main menu.")
			}
			continue
		case "r":
			current.loaded = false
			continue
		case "h":
			s.WriteLine("Choose a numbered resource or a letter action. /word filters names; / clears the filter. Ctrl-C returns to the parent; q exits only the menu.")
			for _, item := range fixedActions(current.view) {
				s.Printf("%s: %s", item.Key, output.RedactError(item.Label))
				if item.Impact != "" {
					s.Printf(" — %s", output.RedactError(impactDescription(item.Impact)))
				}
				s.WriteLine("")
			}
			continue
		case "n":
			if (current.index+1)*pageSize < len(resources) {
				current.index++
				current.render = true
			} else {
				s.WriteLine("Already at the last page.")
			}
			continue
		case "p":
			if current.index > 0 {
				current.index--
				current.render = true
			} else {
				s.WriteLine("Already at the first page.")
			}
			continue
		}
		var item Item
		found := false
		for _, action := range fixedActions(current.view) {
			if value == action.Key {
				item = action
				found = true
				break
			}
		}
		if !found {
			n, e := strconv.Atoi(value)
			start := current.index * pageSize
			if e == nil && n >= 1 && n <= pageSize && start+n <= len(resources) {
				item = resources[start+n-1]
				found = true
			}
		}
		if !found {
			s.WriteLine("Choose a listed number or letter, /filter, r, h, b, or q.")
			continue
		}
		if item.Next != nil {
			stack = append(stack, frame{page: item.Next(), render: true})
			continue
		}
		nav := Navigation{}
		if item.Navigate != nil {
			nav, err = item.Navigate(ctx, s)
		} else if item.Action != nil {
			err = item.Action(ctx, s)
		} else if len(item.Args) > 0 {
			s.Run(ctx, item.Args...)
			err = nil
		}
		if errors.Is(err, ErrQuit) {
			return 0
		}
		if err != nil && !errors.Is(err, ErrBack) {
			s.Printf("Error: %s\n", output.RedactError(err.Error()))
		}
		current.loaded = false
		if err == nil && nav.Back && len(stack) > 1 {
			stack = stack[:len(stack)-1]
			stack[len(stack)-1].loaded = false
		} else if err == nil && nav.Next != nil {
			stack = append(stack, frame{page: nav.Next, render: true})
		}
	}
	return 0
}

// The terminal is canonical: readiness means a complete line is available.
// Polling in the same goroutine avoids abandoned readers after cancellation.
func (s *Session) interruptibleRead() (string, error) {
	select {
	case <-s.interrupts:
		fmt.Fprintln(s.out)
		return "", ErrBack
	default:
	}
	if s.in.Buffered() > 0 || s.file == nil || !IsTerminal(s.file) {
		return s.in.ReadString('\n')
	}
	temporary := false
	if s.interrupts == nil {
		s.interrupts = make(chan os.Signal, 8)
		signal.Notify(s.interrupts, os.Interrupt)
		temporary = true
	}
	if temporary {
		defer func() { signal.Stop(s.interrupts); s.interrupts = nil }()
	}
	for {
		select {
		case <-s.interrupts:
			fmt.Fprintln(s.out)
			return "", ErrBack
		default:
		}
		fd := s.file.Fd()
		n, errno := terminalReady(fd)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return "", errno
		}
		if n > 0 {
			return s.in.ReadString('\n')
		}
	}
}

func (s *Session) discardInterrupts() {
	for {
		select {
		case <-s.interrupts:
		default:
			return
		}
	}
}

// IsTerminal checks that a file is attached to an interactive terminal.
func IsTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	request := uintptr(0x5401) // Linux TCGETS
	if runtime.GOOS == "darwin" {
		request = 0x40487413 // macOS TIOCGETA
	}
	var state syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), request, uintptr(unsafe.Pointer(&state)))
	return errno == 0
}
