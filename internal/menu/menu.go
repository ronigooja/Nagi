package menu

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

type Session struct {
	runner Runner
	in     *bufio.Reader
	out    io.Writer
}

func NewSession(runner Runner, in io.Reader, out io.Writer) *Session {
	return &Session{runner: runner, in: bufio.NewReader(in), out: out}
}

func (s *Session) Fetch(ctx context.Context, args ...string) Result {
	return s.runner.Run(ctx, args)
}

func (s *Session) Run(ctx context.Context, args ...string) Result {
	result := s.Fetch(ctx, args...)
	s.showResult(result)
	return result
}

func (s *Session) Stream(ctx context.Context, args ...string) Result {
	streamer, ok := s.runner.(StreamRunner)
	if !ok {
		return s.Run(ctx, args...)
	}
	streamCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
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
	loaded bool
}

// Run drives the line-oriented menu. It returns zero for ordinary exit and
// interruption; command failures remain on the current page.
func Run(ctx context.Context, s *Session) int {
	stack := []frame{{page: RootPage()}}
	for len(stack) > 0 {
		current := &stack[len(stack)-1]
		if !current.loaded {
			if current.page.Load != nil {
				view, err := current.page.Load(ctx, s)
				if err != nil {
					s.Printf("Error refreshing %s: %s\n", current.page.Title, err)
					if len(current.view.Items) == 0 {
						current.view = view
					}
				} else {
					current.view = view
				}
			}
			current.loaded = true
			if current.index*pageSize >= len(current.view.Items) {
				current.index = 0
			}
		}
		s.render(current.page, current.view, current.index, len(stack))
		value, err := s.readLine("Choice: ")
		if errors.Is(err, ErrQuit) || errors.Is(err, context.Canceled) {
			return 0
		}
		if err != nil {
			s.Printf("Input error: %s\n", err)
			return 1
		}
		switch strings.ToLower(value) {
		case "":
			continue
		case "q":
			return 0
		case "0":
			if len(stack) == 1 {
				return 0
			}
		case "b":
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			} else {
				s.WriteLine("Already at the main menu.")
			}
			continue
		case "r":
			current.loaded = false
			continue
		case "h":
			if len(current.page.Help) > 0 {
				s.Run(ctx, append([]string{"help"}, current.page.Help...)...)
			} else {
				s.WriteLine("Choose an action to see its command help.")
			}
			continue
		case "n":
			if (current.index+1)*pageSize < len(current.view.Items) {
				current.index++
			} else {
				s.WriteLine("Already at the last page.")
			}
			continue
		case "p":
			if current.index > 0 {
				current.index--
			} else {
				s.WriteLine("Already at the first page.")
			}
			continue
		}
		n, err := strconv.Atoi(value)
		start := current.index * pageSize
		end := start + pageSize
		if end > len(current.view.Items) {
			end = len(current.view.Items)
		}
		if err != nil || n < 1 || start+n > end {
			s.WriteLine("Choose a listed number, r, h, b, or q.")
			continue
		}
		item := current.view.Items[start+n-1]
		if item.Next != nil {
			stack = append(stack, frame{page: item.Next()})
			continue
		}
		if item.Confirm != "" {
			confirmed, err := s.Confirm(item.Confirm)
			if errors.Is(err, ErrQuit) {
				return 0
			}
			if errors.Is(err, ErrBack) {
				continue
			}
			if err != nil {
				s.Printf("Input error: %s\n", err)
				continue
			}
			if !confirmed {
				s.WriteLine("Cancelled.")
				continue
			}
		}
		if item.Action != nil {
			if err := item.Action(ctx, s); errors.Is(err, ErrQuit) {
				return 0
			} else if err != nil && !errors.Is(err, ErrBack) {
				s.Printf("Error: %s\n", err)
			}
		} else if len(item.Args) > 0 {
			s.Run(ctx, item.Args...)
		}
		current.loaded = false
	}
	return 0
}

func (s *Session) interruptibleRead() (string, error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	type response struct {
		line string
		err  error
	}
	ready := make(chan response, 1)
	go func() { line, err := s.in.ReadString('\n'); ready <- response{line, err} }()
	select {
	case result := <-ready:
		return result.line, result.err
	case <-ctx.Done():
		return "", ErrQuit
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
