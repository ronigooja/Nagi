package menu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/rivo/uniseg"
	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/proxy"
	"github.com/ronigooja/Nagi/internal/terminal"
	"golang.org/x/sys/unix"
)

const liveLineLimit = 8 << 20
const liveEndedLimit = 1000
const liveEndedBytes = 32 << 20
const liveFailedLimit = 1000
const liveFailedBytes = 32 << 20

// liveConnections consumes the existing mihomo-backed watch stream. The view
// keeps only display history; mihomo remains authoritative for live snapshots
// and close requests.
func liveConnections(ctx context.Context, s *Session) error {
	if s.file == nil || !IsTerminal(s.file) {
		s.Stream(ctx, "connections", "watch")
		return nil
	}
	streamer, ok := s.runner.(StreamRunner)
	if !ok {
		s.Stream(ctx, "connections", "watch")
		return nil
	}
	if os.Getenv("TERM") == "dumb" {
		s.Run(ctx, "connections", "list")
		s.longResult = true
		return nil
	}
	restore, err := terminal.Raw(s.file)
	if err != nil {
		s.Stream(ctx, "connections", "watch")
		return nil
	}
	defer restore()
	defer discardLiveInput(s)
	screen := &liveScreen{out: s.out, file: s.file}
	if err := screen.enter(); err != nil {
		return err
	}
	defer screen.leave()

	streamCtx, stop := context.WithCancel(ctx)
	defer stop()
	commands := make(chan liveSample, 1)
	done := make(chan Result, 1)
	go func() {
		writer := &liveLineWriter{ctx: streamCtx, out: commands}
		done <- streamer.Stream(streamCtx, []string{"--json", "connections", "watch"}, writer)
	}()
	streamDone := false
	defer func() {
		stop()
		if !streamDone {
			<-done
		}
		s.discardInterrupts()
	}()

	active := map[string]proxy.Connection{}
	ended := map[string]liveEnded{}
	failed := map[string]liveFailure{}
	closing := map[string]bool{}
	filter, search, sortBy := "all", "", "target"
	page := 0
	frozen := false
	var frozenRows []liveRow
	var frozenActive, frozenEnded, frozenFailed int
	var sampledAt time.Time
	var frozenSampledAt time.Time
	var detail *liveRow
	input := strings.Builder{}
	dirty := true
	urgent := true
	var lastDraw time.Time
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case sample := <-commands:
			if sample.err != nil {
				return sample.err
			}
			mergeLiveObservation(active, ended, failed, sample.connections, sample.failures, len(sample.gaps) != 0, time.Now())
			for id := range closing {
				if _, exists := active[id]; !exists {
					delete(closing, id)
				}
			}
			sampledAt = sample.sampledAt
			// Sampling continues while copying or reading details, without
			// touching the displayed text or the terminal's selection.
			if !frozen && detail == nil {
				dirty = true
			}
		case <-ticker.C:
			if screen.resize() {
				dirty, urgent = true, true
			}
			if dirty && (urgent || time.Since(lastDraw) >= 500*time.Millisecond) {
				rows, ac, ec, fc, shownAt := liveRowsWithFailures(active, ended, failed), len(active), len(ended), len(failed), sampledAt
				if frozen {
					rows, ac, ec, fc, shownAt = frozenRows, frozenActive, frozenEnded, frozenFailed, frozenSampledAt
				}
				var frame strings.Builder
				liveRender(&frame, s.language, rows, filter, search, sortBy, frozen, ac, ec, fc, shownAt, input.String(), page, detail, screen.width, screen.height)
				if err := screen.draw(frame.String()); err != nil {
					return err
				}
				dirty, urgent, lastDraw = false, false, time.Now()
			}
			for readCount := 0; readCount < 512; readCount++ {
				if s.in.Buffered() == 0 {
					ready, errno := terminalReady(s.file.Fd())
					if errno == syscall.EINTR || (errno == 0 && ready == 0) {
						break
					}
					if errno != 0 {
						return errno
					}
				}
				b, readErr := liveReadByte(s)
				if readErr != nil {
					if readErr == io.EOF {
						return ErrQuit
					}
					return readErr
				}
				switch b {
				case 3: // Ctrl-C is handled as a page return; the deferred cancel stops watch.
					return ErrBack
				case 4: // Ctrl-D
					return ErrQuit
				case '\r', '\n':
					if input.Len() > 0 {
						command := input.String()
						input.Reset()
						ac, ec, fc, shownAt := len(active), len(ended), len(failed), sampledAt
						if frozen {
							ac, ec, fc, shownAt = frozenActive, frozenEnded, frozenFailed, frozenSampledAt
						}
						header := liveHeader(s.language, filter, search, frozen, ac, ec, fc, shownAt, screen.width)
						pageSize := liveTablePageSize(screen.width, screen.height, len(header))
						if err := liveCommand(command, s, ctx, active, ended, failed, closing, &filter, &search, &sortBy, &frozen, &frozenRows, &frozenActive, &frozenEnded, &frozenFailed, &frozenSampledAt, sampledAt, &page, &detail, pageSize); err != nil {
							return err
						}
						dirty, urgent = true, true
					}
				case 8, 127:
					if input.Len() > 0 {
						text := []rune(input.String())
						input.Reset()
						if len(text) > 1 {
							input.WriteString(string(text[:len(text)-1]))
						}
					}
					dirty, urgent = true, true
				default:
					if b >= 0x20 && b != 0x7f && input.Len() < 512 {
						input.WriteByte(b)
						dirty, urgent = true, true
					}
				}
			}
		case result := <-done:
			streamDone = true
			if result.Error != nil {
				return fmt.Errorf("%s", result.Error.Message)
			}
			return nil
		case <-s.interrupts:
			return ErrBack
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func discardLiveInput(s *Session) {
	for s.in.Buffered() > 0 {
		_, _ = s.in.ReadByte()
	}
	_ = terminal.FlushInput(s.file)
}

func liveReadByte(s *Session) (byte, error) {
	if s.in.Buffered() > 0 {
		return s.in.ReadByte()
	}
	var one [1]byte
	n, err := syscall.Read(int(s.file.Fd()), one[:])
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, io.EOF
	}
	return one[0], nil
}

type liveEnded struct {
	connection proxy.Connection
	when       time.Time
}
type liveFailure struct {
	connection  proxy.Connection
	reason      string
	details     string
	interrupted bool
	when        time.Time
}
type liveRow struct {
	connection  proxy.Connection
	ended       bool
	failed      bool
	reason      string
	details     string
	interrupted bool
	when        time.Time
}
type liveSample struct {
	connections map[string]proxy.Connection
	failures    []liveFailureEvent
	gaps        []string
	sampledAt   time.Time
	err         error
}
type liveFailureEvent struct {
	id         string
	reason     string
	details    string
	connection proxy.Connection
}
type liveLineWriter struct {
	ctx        context.Context
	out        chan<- liveSample
	pending    strings.Builder
	beforeSend chan struct{}
}

func (w *liveLineWriter) Write(p []byte) (int, error) {
	for _, b := range p {
		if err := w.ctx.Err(); err != nil {
			return 0, err
		}
		if b != '\n' {
			if w.pending.Len() >= liveLineLimit {
				return 0, fmt.Errorf("connection sample exceeds %d bytes", liveLineLimit)
			}
			w.pending.WriteByte(b)
			continue
		}
		var envelope struct {
			OK   bool `json:"ok"`
			Data struct {
				Connections   []proxy.Connection `json:"connections"`
				SampledAt     string             `json:"sampled_at"`
				FailureEvents []struct {
					ID         string            `json:"id"`
					Reason     string            `json:"reason"`
					Details    string            `json:"details"`
					Detail     string            `json:"detail"`
					Connection *proxy.Connection `json:"connection"`
				} `json:"failure_events"`
				Gaps []struct {
					Reason  string `json:"reason"`
					Message string `json:"message"`
				} `json:"gaps"`
			} `json:"data"`
			Error *CLIError `json:"error"`
		}
		if err := json.Unmarshal([]byte(w.pending.String()), &envelope); err != nil {
			return 0, err
		}
		w.pending.Reset()
		if !envelope.OK {
			return 0, fmt.Errorf("connection sample failed")
		}
		sample := liveSample{connections: map[string]proxy.Connection{}, sampledAt: time.Now()}
		if parsed, err := time.Parse(time.RFC3339Nano, envelope.Data.SampledAt); err == nil {
			sample.sampledAt = parsed
		}
		for _, connection := range envelope.Data.Connections {
			sample.connections[connection.ID] = connection
		}
		for _, failure := range envelope.Data.FailureEvents {
			if failure.ID == "" {
				continue
			}
			if failure.Details == "" {
				failure.Details = failure.Detail
			}
			value := proxy.Connection{ID: failure.ID}
			if failure.Connection != nil {
				value = *failure.Connection
				value.ID = failure.ID
			}
			sample.failures = append(sample.failures, liveFailureEvent{id: failure.ID, reason: failure.Reason, details: failure.Details, connection: value})
		}
		for _, gap := range envelope.Data.Gaps {
			reason := gap.Reason
			if reason == "" {
				reason = gap.Message
			}
			sample.gaps = append(sample.gaps, reason)
		}
		if w.beforeSend != nil {
			close(w.beforeSend)
			w.beforeSend = nil
		}
		select {
		case w.out <- sample:
		case <-w.ctx.Done():
			return 0, w.ctx.Err()
		}
	}
	return len(p), nil
}

func liveRows(active map[string]proxy.Connection, ended map[string]liveEnded) []liveRow {
	return liveRowsWithFailures(active, ended, nil)
}

func liveRowsWithFailures(active map[string]proxy.Connection, ended map[string]liveEnded, failed map[string]liveFailure) []liveRow {
	rows := make([]liveRow, 0, len(active)+len(ended)+len(failed))
	for _, c := range active {
		rows = append(rows, liveRow{connection: c, interrupted: liveConnectionInterrupted(c)})
	}
	for _, e := range ended {
		rows = append(rows, liveRow{connection: e.connection, ended: true, interrupted: liveConnectionInterrupted(e.connection), when: e.when})
	}
	for _, f := range failed {
		rows = append(rows, liveRow{connection: f.connection, failed: true, reason: f.reason, details: f.details, interrupted: f.interrupted, when: f.when})
	}
	return rows
}

func mergeLiveSample(active map[string]proxy.Connection, ended map[string]liveEnded, sample map[string]proxy.Connection, now time.Time) {
	mergeLiveObservation(active, ended, nil, sample, nil, false, now)
}

func mergeLiveObservation(active map[string]proxy.Connection, ended map[string]liveEnded, failed map[string]liveFailure, sample map[string]proxy.Connection, failures []liveFailureEvent, gap bool, now time.Time) {
	for id, old := range active {
		if !gap {
			if _, ok := sample[id]; !ok {
				ended[id] = liveEnded{connection: old, when: now}
				delete(active, id)
			}
		}
	}
	for id, current := range sample {
		delete(ended, id)
		if failed != nil {
			delete(failed, id)
		}
		active[id] = current
	}
	if gap {
		for id, current := range active {
			if current.Metadata == nil {
				current.Metadata = map[string]any{}
			}
			current.Metadata["_nagi_observation_interrupted"] = true
			active[id] = current
		}
		for id, current := range ended {
			if current.connection.Metadata == nil {
				current.connection.Metadata = map[string]any{}
			}
			current.connection.Metadata["_nagi_observation_interrupted"] = true
			ended[id] = current
		}
	}
	for id, old := range ended {
		if now.Sub(old.when) >= 5*time.Minute {
			delete(ended, id)
		}
	}
	type sizedEntry struct {
		id   string
		when time.Time
		size int
	}
	entries := make([]sizedEntry, 0, len(ended))
	total := 2
	for id, entry := range ended {
		data, _ := json.Marshal(entry.connection)
		entries = append(entries, sizedEntry{id: id, when: entry.when, size: len(data)})
		total += len(data) + 1
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].when.Equal(entries[j].when) {
			return entries[i].id < entries[j].id
		}
		return entries[i].when.Before(entries[j].when)
	})
	for len(ended) > liveEndedLimit || total > liveEndedBytes {
		if len(entries) == 0 {
			break
		}
		oldest := entries[0]
		entries = entries[1:]
		delete(ended, oldest.id)
		total -= oldest.size + 1
	}
	if failed != nil {
		for _, event := range failures {
			failed[event.id] = liveFailure{connection: event.connection, reason: event.reason, details: event.details, when: now}
			delete(active, event.id)
			delete(ended, event.id)
		}
		for id, entry := range failed {
			if now.Sub(entry.when) >= 5*time.Minute {
				delete(failed, id)
			}
		}
		if gap {
			for id, entry := range failed {
				entry.interrupted = true
				failed[id] = entry
			}
		}
		pruneLiveFailures(failed)
	}
}

func liveConnectionInterrupted(c proxy.Connection) bool {
	value, ok := c.Metadata["_nagi_observation_interrupted"].(bool)
	return ok && value
}

func pruneLiveFailures(failed map[string]liveFailure) {
	type sized struct {
		id   string
		when time.Time
		size int
	}
	entries := make([]sized, 0, len(failed))
	total := 0
	for id, entry := range failed {
		data, _ := json.Marshal(entry)
		entries = append(entries, sized{id, entry.when, len(data)})
		total += len(data)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].when.Equal(entries[j].when) {
			return entries[i].id < entries[j].id
		}
		return entries[i].when.Before(entries[j].when)
	})
	for len(failed) > liveFailedLimit || total > liveFailedBytes {
		if len(entries) == 0 {
			return
		}
		oldest := entries[0]
		entries = entries[1:]
		delete(failed, oldest.id)
		total -= oldest.size
	}
}

func liveCommand(command string, s *Session, ctx context.Context, active map[string]proxy.Connection, ended map[string]liveEnded, failed map[string]liveFailure, closing map[string]bool, filter, search, sortBy *string, frozen *bool, frozenRows *[]liveRow, frozenActive, frozenEnded, frozenFailed *int, frozenSampledAt *time.Time, sampledAt time.Time, page *int, detail **liveRow, pageSizes ...int) error {
	cmd := strings.TrimSpace(command)
	switch {
	case cmd == "q" || cmd == "0":
		return ErrBack
	case cmd == "x":
		*detail = nil
	case cmd == "f":
		*page = 0
		if *filter == "all" {
			*filter = "active"
		} else if *filter == "active" {
			*filter = "ended"
		} else if *filter == "ended" {
			*filter = "failed"
		} else {
			*filter = "all"
		}
	case cmd == "s":
		if *sortBy == "target" {
			*sortBy = "traffic"
		} else {
			*sortBy = "target"
		}
	case cmd == "p":
		*frozen = !*frozen
		if *frozen {
			*frozenRows, *frozenActive, *frozenEnded, *frozenFailed = liveRowsWithFailures(active, ended, failed), len(active), len(ended), len(failed)
			*frozenSampledAt = sampledAt
		}
	case strings.HasPrefix(cmd, "/"):
		*search = strings.TrimPrefix(cmd, "/")
		*page = 0
	case cmd == "n":
		rows := liveRowsWithFailures(active, ended, failed)
		if *frozen {
			rows = *frozenRows
		}
		filtered := liveFilteredRows(rows, *filter, *search, *sortBy)
		pages := livePageCount(filtered, pageSizes...)
		// A resize or changed filter may have clamped the displayed page.
		*page = max(0, min(*page, pages-1))
		if *page+1 < pages {
			*page++
		}
	case cmd == "b":
		if *detail != nil {
			*detail = nil
		} else {
			rows := liveRowsWithFailures(active, ended, failed)
			if *frozen {
				rows = *frozenRows
			}
			pages := livePageCount(liveFilteredRows(rows, *filter, *search, *sortBy), pageSizes...)
			*page = max(0, min(*page, pages-1)-1)
		}
	case strings.HasPrefix(cmd, "d "):
		rows := liveRowsWithFailures(active, ended, failed)
		if *frozen {
			rows = *frozenRows
		}
		id := strings.TrimSpace(strings.TrimPrefix(cmd, "d "))
		for _, row := range rows {
			if row.connection.ID == id {
				copy := row
				*detail = &copy
				break
			}
		}
	case strings.HasPrefix(cmd, "c "):
		if *frozen {
			return nil
		}
		id := strings.TrimSpace(strings.TrimPrefix(cmd, "c "))
		if _, ok := active[id]; ok && !closing[id] {
			// Do not emit ordinary command feedback into the full-screen view.
			result := s.Fetch(ctx, "connections", "close", id)
			s.discardInterrupts()
			if result.Error != nil {
				return fmt.Errorf("%s", result.Error.Message)
			}
			if result.Code != 0 {
				return fmt.Errorf("connection close failed")
			}
			closing[id] = true
		}
	}
	return nil
}

func liveDetail(out io.Writer, language i18n.Context, row liveRow) {
	status := menuText(language, "menu.traffic.live.detail_active", "Active")
	if row.ended {
		status = menuText(language, "menu.traffic.live.detail_ended", "Ended; end time and final counters are unavailable.")
	}
	if row.failed {
		status = menuText(language, "menu.traffic.live.detail_failed", "Failed connection attempt; this record cannot be closed.")
	}
	c := row.connection
	fmt.Fprintf(out, "\n%s\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n", status,
		menuText(language, "menu.traffic.live.id", "ID"), liveSafe(c.ID),
		menuText(language, "menu.traffic.live.target", "Target"), liveSafe(liveTarget(c)),
		menuText(language, "menu.traffic.live.process", "Process"), liveSafe(fmt.Sprint(c.Metadata["process"])),
		menuText(language, "menu.traffic.live.protocol", "Protocol"), liveSafe(fmt.Sprint(c.Metadata["network"])),
		menuText(language, "menu.traffic.live.proxy_chain", "Proxy chain"), liveSafe(strings.Join(c.Chains, " → ")),
		menuText(language, "menu.traffic.live.upload", "Upload"), humanBytes(c.Upload),
		menuText(language, "menu.traffic.live.download", "Download"), humanBytes(c.Download))
	if row.failed {
		fmt.Fprintf(out, "%s: %s\n", menuText(language, "menu.traffic.live.reason", "Reason"), liveSafe(row.reason))
		if row.details != "" {
			fmt.Fprintf(out, "%s: %s\n", menuText(language, "menu.traffic.live.details", "Details"), liveSafe(row.details))
		}
	}
	if row.interrupted {
		fmt.Fprintf(out, "%s\n", menuText(language, "menu.traffic.live.interrupted", "Observation interrupted; values may be stale."))
	}
	fmt.Fprintf(out, "[x] %s\n", menuText(language, "menu.traffic.live.back", "back to table"))
}

// liveScreen uses cursor addressing rather than newlines, so even a full frame
// cannot scroll. NO_COLOR controls styling, not cursor movement. Mouse reporting
// remains disabled: the terminal retains its normal text selection behavior.
type liveScreen struct {
	out           io.Writer
	file          *os.File
	width, height int
	previous      []string
}

func (s *liveScreen) enter() error {
	_, err := io.WriteString(s.out, "\x1b[?1049h\x1b[H\x1b[2J")
	if err != nil {
		s.leave()
	}
	return err
}

func (s *liveScreen) leave() {
	_, _ = io.WriteString(s.out, "\x1b[?25h\x1b[?1049l")
}

func (s *liveScreen) resize() bool {
	width, height := 80, 24
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && n > 0 {
		width = n
	}
	if n, err := strconv.Atoi(os.Getenv("LINES")); err == nil && n > 0 {
		height = n
	}
	if s.file != nil {
		if size, err := unix.IoctlGetWinsize(int(s.file.Fd()), unix.TIOCGWINSZ); err == nil {
			if size.Col > 0 {
				width = int(size.Col)
			}
			if size.Row > 0 {
				height = int(size.Row)
			}
		}
	}
	if width == s.width && height == s.height {
		return false
	}
	s.width, s.height, s.previous = width, height, nil
	return true
}

func (s *liveScreen) draw(frame string) error {
	lines := strings.Split(strings.TrimSuffix(frame, "\n"), "\n")
	if len(lines) > s.height {
		lines = lines[:s.height]
	}
	var update strings.Builder
	for row := 0; row < s.height; row++ {
		line := ""
		if row < len(lines) {
			line = liveClip(liveSafe(lines[row]), max(0, s.width-1))
			lines[row] = line
		}
		if row < len(s.previous) && s.previous[row] == line {
			continue
		}
		if update.Len() == 0 {
			update.WriteString("\x1b[?25l")
		}
		fmt.Fprintf(&update, "\x1b[%d;1H\x1b[2K%s", row+1, line)
	}
	if update.Len() == 0 {
		return nil
	}
	// Keep the cursor next to the command, away from the terminal's last cell.
	last := ""
	if len(lines) > 0 {
		last = lines[len(lines)-1]
	}
	fmt.Fprintf(&update, "\x1b[%d;%dH\x1b[?25h", max(1, len(lines)), min(s.width, uniseg.StringWidth(last)+1))
	_, err := io.WriteString(s.out, update.String())
	if err == nil {
		s.previous = make([]string, s.height)
		copy(s.previous, lines)
	}
	return err
}

func liveWrapItems(text string, width int) []string {
	var lines []string
	line := ""
	for _, item := range strings.Split(text, "  ") {
		if item == "" {
			continue
		}
		if line != "" && uniseg.StringWidth(line)+2+uniseg.StringWidth(item) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += "  "
		}
		line += item
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func liveHeader(language i18n.Context, filter, query string, frozen bool, activeCount, endedCount, failedCount int, sampled time.Time, width int) []string {
	width = max(1, width-1)
	lines := []string{menuText(language, "menu.traffic.live.instructions", "Connections")}
	for _, control := range []string{
		menuText(language, "menu.traffic.live.controls_view", "[f] Filter  /text Search  [s] Sort  [p] Freeze/resume"),
		menuText(language, "menu.traffic.live.controls_actions", "[d ID] Details  [c ID] Close  [n/b] Page"),
		menuText(language, "menu.traffic.live.controls_navigation", "[x] Table  [q] Back  Enter to apply"),
	} {
		lines = append(lines, liveWrapItems(control, width)...)
	}
	filterLabel := menuText(language, "menu.traffic.live.filter_all", "All")
	if filter == "active" {
		filterLabel = menuText(language, "menu.traffic.live.filter_active", "Active")
	}
	if filter == "ended" {
		filterLabel = menuText(language, "menu.traffic.live.filter_ended", "Ended")
	}
	if filter == "failed" {
		filterLabel = menuText(language, "menu.traffic.live.filter_failed", "Failed")
	}
	timeLabel := "—"
	if !sampled.IsZero() {
		timeLabel = sampled.Format("15:04")
	}
	summary := fmt.Sprintf("%s: %s  %s: %d  %s: %d  %s: %d  %s: %s",
		menuText(language, "menu.traffic.live.filter", "Filter"), filterLabel,
		menuText(language, "menu.traffic.live.active", "Active"), activeCount,
		menuText(language, "menu.traffic.live.ended", "Recently ended"), endedCount,
		menuText(language, "menu.traffic.live.failed", "Failed"), failedCount,
		menuText(language, "menu.traffic.live.sampled", "Sampled"), timeLabel)
	lines = append(lines, liveWrapItems(summary, width)...)
	if frozen {
		lines = append(lines, menuText(language, "menu.traffic.live.frozen", "Frozen; select text to copy. Collection continues."))
	}
	if query != "" {
		lines = append(lines, "/"+liveSafe(query))
	}
	return lines
}

func liveTablePageSize(width, height, headerLines int) int {
	columns, record := 1, 1
	if width < 120 {
		columns, record = 2, 2
	}
	return max(1, (height-headerLines-columns-2)/record)
}

func liveRender(out io.Writer, language i18n.Context, rows []liveRow, filter, query, sortBy string, frozen bool, activeCount, endedCount, failedCount int, sampled time.Time, input string, page int, detail *liveRow, width, height int) {
	lines := liveHeader(language, filter, query, frozen, activeCount, endedCount, failedCount, sampled, width)
	if detail != nil {
		var body strings.Builder
		liveDetail(&body, language, *detail)
		lines = append(lines, strings.Split(strings.TrimSpace(body.String()), "\n")...)
	} else {
		filtered := liveFilteredRows(rows, filter, query, sortBy)
		pageSize := liveTablePageSize(width, height, len(lines))
		pages := max(1, livePageCount(filtered, pageSize))
		page = max(0, min(page, pages-1))
		// Compact terminals use two physical rows per connection. The full ID
		// stays on its own line instead of forcing every table row to wrap.
		compact := width < 120
		idWidth := 36
		for _, row := range rows {
			idWidth = max(idWidth, uniseg.StringWidth(liveSafe(row.connection.ID)))
		}
		if compact {
			lines = append(lines, menuText(language, "menu.traffic.live.id", "ID")+" / "+menuText(language, "menu.traffic.live.state", "State"))
		}
		targetWidth := max(1, width-1-8-12-8-8-5)
		if !compact {
			targetWidth = max(1, targetWidth-idWidth-1-7)
		}
		format := func(target, protocol, chain, upload, download string) string {
			return fmt.Sprintf("%s %s %s %s/%s", liveFit(target, targetWidth), liveFit(protocol, 8), liveFit(chain, 12), liveFit(upload, 8), liveFit(download, 8))
		}
		columns := format(menuText(language, "menu.traffic.live.target", "Target")+" / "+menuText(language, "menu.traffic.live.process", "Process"),
			menuText(language, "menu.traffic.live.protocol", "Protocol"), menuText(language, "menu.traffic.live.proxy_chain", "Proxy chain"),
			menuText(language, "menu.traffic.live.upload", "Upload"), menuText(language, "menu.traffic.live.download", "Download"))
		if !compact {
			columns = liveFit("ID", idWidth) + " " + columns + " " + menuText(language, "menu.traffic.live.state", "State")
		}
		lines = append(lines, columns)
		start, end := page*pageSize, min((page+1)*pageSize, len(filtered))
		for _, row := range filtered[start:end] {
			c := row.connection
			state := menuText(language, "menu.traffic.live.active_state", "ACTIVE")
			if row.ended {
				state = menuText(language, "menu.traffic.live.ended_state", "ENDED")
			}
			if row.failed {
				state = menuText(language, "menu.traffic.live.failed_state", "FAILED")
			}
			values := format(liveSafe(liveTarget(c)), liveSafe(fmt.Sprint(c.Metadata["network"])), liveSafe(strings.Join(c.Chains, " → ")), humanBytes(c.Upload), humanBytes(c.Download))
			if compact {
				lines = append(lines, liveSafe(c.ID)+"  "+state, values)
			} else {
				lines = append(lines, liveFit(liveSafe(c.ID), idWidth)+" "+values+" "+state)
			}
		}
		lines = append(lines, fmt.Sprintf("%s %d/%d", menuText(language, "menu.traffic.live.page", "Page"), page+1, pages))
	}
	// Reserve the final terminal row for input even on very small terminals.
	if len(lines) >= height {
		lines = lines[:max(0, height-1)]
	}
	for len(lines) < height-1 {
		lines = append(lines, "")
	}
	lines = append(lines, liveInputViewport(input, max(0, width-1)))
	for _, line := range lines {
		fmt.Fprintln(out, liveClip(liveSafe(line), max(0, width-1)))
	}
}

func liveInputViewport(input string, width int) string {
	if width < 2 {
		return liveClip("> ", width)
	}
	value := liveSafe(input)
	g := uniseg.NewGraphemes(value)
	remaining, start := uniseg.StringWidth(value), 0
	for remaining > width-2 && g.Next() {
		_, start = g.Positions()
		remaining -= g.Width()
	}
	return "> " + value[start:]
}

func liveRowsPerPage() int {
	if n, err := strconv.Atoi(os.Getenv("LINES")); err == nil && n > 0 {
		return max(1, n-7)
	}
	return 12
}

func livePageCount(rows []liveRow, sizes ...int) int {
	n := liveRowsPerPage()
	if len(sizes) > 0 {
		n = max(1, sizes[0])
	}
	return (len(rows) + n - 1) / n
}

func liveFilteredRows(rows []liveRow, filter, query, sortBy string) []liveRow {
	filtered := make([]liveRow, 0, len(rows))
	for _, r := range rows {
		if filter == "active" && (r.ended || r.failed) || filter == "ended" && (r.ended == false || r.failed) || filter == "failed" && !r.failed {
			continue
		}
		text := strings.ToLower(fmt.Sprint(r.connection.ID, " ", r.connection.Metadata, " ", r.connection.Chains, " ", r.reason, " ", r.details))
		if query != "" && !strings.Contains(text, strings.ToLower(query)) {
			continue
		}
		filtered = append(filtered, r)
	}
	if sortBy == "traffic" {
		sort.Slice(filtered, func(i, j int) bool {
			a, b := filtered[i].connection, filtered[j].connection
			aa, bb := a.Upload+a.Download, b.Upload+b.Download
			if aa == bb {
				return a.ID < b.ID
			}
			return aa > bb
		})
	} else {
		sort.Slice(filtered, func(i, j int) bool {
			a, b := filtered[i].connection, filtered[j].connection
			x, y := liveTarget(a), liveTarget(b)
			if x == y {
				return a.ID < b.ID
			}
			return x < y
		})
	}
	return filtered
}
func liveClip(text string, width int) string {
	var b strings.Builder
	g := uniseg.NewGraphemes(text)
	cells := 0
	for g.Next() {
		if cells+g.Width() > width {
			break
		}
		b.WriteString(g.Str())
		cells += g.Width()
	}
	return b.String()
}

func liveFit(text string, width int) string {
	text = liveClip(text, width)
	return text + strings.Repeat(" ", max(0, width-uniseg.StringWidth(text)))
}

func liveTarget(c proxy.Connection) string {
	host := fmt.Sprint(c.Metadata["host"])
	if host == "<nil>" || host == "" {
		host = fmt.Sprint(c.Metadata["destinationIP"])
	}
	port := fmt.Sprint(c.Metadata["destinationPort"])
	if port != "<nil>" && port != "" {
		host += ":" + port
	}
	process := fmt.Sprint(c.Metadata["process"])
	if process != "<nil>" && process != "" {
		host += " / " + process
	}
	return host
}

func liveSafe(value string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) || r == 0x1b {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func humanBytes(n int64) string {
	const unit = int64(1024)
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	value, suffix := float64(n), "B"
	for _, next := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= float64(unit)
		suffix = next
		if value < float64(unit) {
			break
		}
	}
	return fmt.Sprintf("%.1f%s", value, suffix)
}
