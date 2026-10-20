package menu

import "github.com/ronigooja/Nagi/internal/i18n"

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
	"syscall"

	"github.com/ronigooja/Nagi/internal/terminal"
)

type Session struct {
	language   i18n.Context
	runner     Runner
	in         *bufio.Reader
	out        io.Writer
	file       *os.File
	interrupts chan os.Signal
	active     bool
	pending    string
	longResult bool
	styles     bool
}

func NewSession(runner Runner, in io.Reader, out io.Writer) *Session {
	return NewSessionWithLanguage(runner, in, out, i18n.New(i18n.English))
}

// NewSessionWithLanguage fixes the language for the complete menu lifetime.
func NewSessionWithLanguage(runner Runner, in io.Reader, out io.Writer, language i18n.Context) *Session {
	if concrete, ok := runner.(*commandRunner); ok {
		clone := *concrete
		clone.language = &language
		runner = &clone
	}
	s := &Session{language: language, runner: runner, in: bufio.NewReader(in), out: out, styles: shouldStyle(out)}
	s.file, _ = in.(*os.File)
	return s
}

func (s *Session) Fetch(ctx context.Context, args ...string) Result {
	return s.runner.Run(ctx, args)
}

// fetchReads preserves serial behavior for runners without batch support.
func (s *Session) fetchReads(ctx context.Context, commands [][]string) []Result {
	if runner, ok := s.runner.(ReadRunner); ok {
		return runner.Read(ctx, commands)
	}
	results := make([]Result, len(commands))
	for i, args := range commands {
		results[i] = s.Fetch(ctx, args...)
	}
	return results
}

func (s *Session) Run(ctx context.Context, args ...string) Result {
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
		s.ShowResult(args, result)
	}
	return result
}

var registered = map[string]func() *Page{}

// RegisterPage connects a feature page to the root menu without coupling the
// core to a feature's implementation file.
func RegisterPage(key string, factory func() *Page) { registered[key] = factory }

type rootEntry struct{ key, label string }

var rootEntries = []rootEntry{
	{"service", "Service"},
	{"traffic", "Network Access"},
	{"proxy", "Proxies"},
	{"routing", "Routing & DNS"},
	{"profiles", "Profiles & Subscriptions"},
	{"rewrite-capture", "Rewrite & Capture"},
	{"diagnostics", "Monitoring & Diagnostics"},
	{"maintenance", "Maintenance"},
}

func localizedRootPage(language i18n.Context) *Page {
	return localizedrootPageForOS(language, runtime.GOOS)
}

func localizedrootPageForOS(language i18n.Context, goos string) *Page {
	return &Page{ID: "menu.rootPageForOS.page1", Root: true, Title: menuText(language, "menu.menu.root_page_for_o_s.nagi_menu", "Nagi menu"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: s.statusSummary(s.Fetch(ctx, "status"))}
		for _, entry := range rootEntries {
			entry := entry
			factory := registered[entry.key]
			if factory == nil {
				continue
			}
			switch entry.key {
			case "proxy":
				factory = func() *Page { return localizedProxyPage(language) }
			case "routing":
				factory = func() *Page { return localizedRoutingPage(language) }
			case "profiles":
				factory = func() *Page { return localizedProfilesAndSubscriptionsPage(language) }
			case "traffic":
				factory = func() *Page { return localizedtrafficPageForOS(language, goos) }
			case "rewrite-capture":
				factory = func() *Page { return localizedrewriteAndCapturePageForOS(language, goos) }
			case "diagnostics":
				factory = func() *Page { return localizeddiagnosticsPageForOS(language, goos) }
			case "service":
				factory = func() *Page { return localizedserviceManagementPageForOS(language, goos) }
			case "maintenance":
				factory = func() *Page { return localizedmaintenancePageForOS(language, goos) }
			}
			view.Items = append(view.Items, Item{ID: "root." + entry.key, Label: menuText(language, "menu.root."+entry.key, entry.label), Next: factory})
		}
		return view, nil
	}}
}

type frame struct {
	page   *Page
	view   PageView
	index  int
	loaded bool
	render bool
}

// Run drives the scrollback-preserving menu; operations remain CLI commands.
func Run(ctx context.Context, s *Session) int {
	return RunPage(ctx, s, localizedRootPage(s.language))
}

func RunPage(ctx context.Context, s *Session, root *Page) int {
	s.active = true
	defer func() { s.active = false }()
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
					s.feedback(feedbackError, menuFormat(s.language, "menu.menu.run_page.error_reading_s_s", "Error reading %s: %s", current.page.Title, err.Error()))
					view.Items = nil // Failed reads cannot leave stale resources selectable.
					view.Empty = view.ReadErrorHint
					if view.Empty == "" {
						view.Empty = menuText(s.language, "menu.menu.run_page.data_unavailable_return_to_the_parent_page_and_r", "Data unavailable. Return to the parent page and reopen to try again.")

					}
				}
				current.view = view
			}
			current.loaded = true
			last := (len(current.view.Items) - 1) / pageSize
			if current.index > last {
				current.index = last
			}
			if current.index < 0 {
				current.index = 0
			}
			current.render = true
		}
		if current.render {
			s.render(current.page, current.view, current.index, len(stack))
			current.render = false
			s.flushResult()
		}
		value, err := s.readChoice()
		if errors.Is(err, ErrQuit) || errors.Is(err, context.Canceled) {
			return 0
		}
		if errors.Is(err, ErrBack) {
			value = "0"
			if len(stack) == 1 {
				return 0
			}
		} else if err != nil {
			s.writeFeedback(feedbackError, menuFormat(s.language, "menu.menu.run_page.input_error_s", "Input error: %s", err))
			return 1
		}
		if value == "" {
			continue
		}
		if value == "0" {
			if len(stack) == 1 {
				return 0
			}
			stack = stack[:len(stack)-1]
			stack[len(stack)-1].loaded = false
			continue
		}
		choices := localizednumberedChoices(s.language,
			current.view, current.index)
		n, e := strconv.Atoi(value)
		if e != nil || n < 1 || n > len(choices) || strconv.Itoa(n) != value {
			s.writeFeedback(feedbackWarning, menuText(s.language, "menu.menu.run_page.choose_a_listed_number", "Choose a listed number."))
			continue
		}
		choice := choices[n-1]
		if choice.PageDelta != 0 {
			current.index += choice.PageDelta
			current.render = true
			continue
		}
		item := choice.Item
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
			s.feedback(feedbackError, menuText(s.language, "menu.menu.run_page.error", "Error: ")+
				err.Error())
		}
		if s.longResult {
			s.flushResult()
			s.longResult = false
			_, pauseErr := s.readLine(menuText(s.language, "menu.menu.run_page.press_enter_to_return", "Press Enter to return: "))
			if errors.Is(pauseErr, ErrQuit) {
				return 0
			}
			if pauseErr != nil && !errors.Is(pauseErr, ErrBack) {
				return 1
			}
		}
		current.loaded = false
		if err == nil && nav.ReturnTo != "" {
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].page.ID == nav.ReturnTo {
					stack = stack[:i+1]
					stack[i].loaded = false
					break
				}
			}
		} else if err == nil && nav.Back && len(stack) > 1 {
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
	return terminal.IsTerminal(file)
}
func RootPage() *Page {
	return localizedRootPage(i18n.New(i18n.English))
}
func rootPageForOS(goos string) *Page {
	return localizedrootPageForOS(i18n.New(i18n.English), goos)
}
