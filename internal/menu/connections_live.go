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

	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/proxy"
	"github.com/ronigooja/Nagi/internal/terminal"
)

const liveLineLimit = 8 << 20
const liveEndedLimit = 1000
const liveEndedBytes = 32 << 20

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
	restore, err := terminal.Raw(s.file)
	if err != nil {
		s.Stream(ctx, "connections", "watch")
		return nil
	}
	defer restore()
	defer discardLiveInput(s)

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
	closing := map[string]bool{}
	filter, search, sortBy := "all", "", "target"
	page := 0
	frozen := false
	var frozenRows []liveRow
	var frozenActive, frozenEnded int
	var sampledAt time.Time
	var frozenSampledAt time.Time
	var detail *liveRow
	input := strings.Builder{}
	dirty := true
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case sample := <-commands:
			if sample.err != nil {
				return sample.err
			}
			mergeLiveSample(active, ended, sample.connections, time.Now())
			for id := range closing {
				if _, exists := active[id]; !exists {
					delete(closing, id)
				}
			}
			sampledAt, dirty = sample.sampledAt, true
		case <-ticker.C:
			if dirty {
				rows, ac, ec, shownAt := liveRows(active, ended), len(active), len(ended), sampledAt
				if frozen {
					rows, ac, ec, shownAt = frozenRows, frozenActive, frozenEnded, frozenSampledAt
				}
				liveRender(s.out, s.language, rows, filter, search, sortBy, frozen, ac, ec, shownAt, input.String(), page, detail)
				dirty = false
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
						if err := liveCommand(command, s, ctx, active, ended, closing, &filter, &search, &sortBy, &frozen, &frozenRows, &frozenActive, &frozenEnded, &frozenSampledAt, sampledAt, &page, &detail); err != nil {
							return err
						}
						s.flushResult()
						dirty = true
					}
				case 8, 127:
					if input.Len() > 0 {
						text := []rune(input.String())
						input.Reset()
						if len(text) > 1 {
							input.WriteString(string(text[:len(text)-1]))
						}
					}
					dirty = true
				default:
					if b >= 0x20 && b != 0x7f && input.Len() < 512 {
						input.WriteByte(b)
						dirty = true
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
type liveRow struct {
	connection proxy.Connection
	ended      bool
	when       time.Time
}
type liveSample struct {
	connections map[string]proxy.Connection
	sampledAt   time.Time
	err         error
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
				Connections []proxy.Connection `json:"connections"`
				SampledAt   string             `json:"sampled_at"`
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
	rows := make([]liveRow, 0, len(active)+len(ended))
	for _, c := range active {
		rows = append(rows, liveRow{connection: c})
	}
	for _, e := range ended {
		rows = append(rows, liveRow{connection: e.connection, ended: true, when: e.when})
	}
	return rows
}

func mergeLiveSample(active map[string]proxy.Connection, ended map[string]liveEnded, sample map[string]proxy.Connection, now time.Time) {
	for id, old := range active {
		if _, ok := sample[id]; !ok {
			ended[id] = liveEnded{connection: old, when: now}
			delete(active, id)
		}
	}
	for id, current := range sample {
		delete(ended, id)
		active[id] = current
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
}

func liveCommand(command string, s *Session, ctx context.Context, active map[string]proxy.Connection, ended map[string]liveEnded, closing map[string]bool, filter, search, sortBy *string, frozen *bool, frozenRows *[]liveRow, frozenActive, frozenEnded *int, frozenSampledAt *time.Time, sampledAt time.Time, page *int, detail **liveRow) error {
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
			*frozenRows, *frozenActive, *frozenEnded = liveRows(active, ended), len(active), len(ended)
			*frozenSampledAt = sampledAt
		}
	case strings.HasPrefix(cmd, "/"):
		*search = strings.TrimPrefix(cmd, "/")
		*page = 0
	case cmd == "n":
		rows := liveRows(active, ended)
		if *frozen {
			rows = *frozenRows
		}
		if max := livePageCount(liveFilteredRows(rows, *filter, *search, *sortBy)); *page+1 < max {
			*page++
		}
	case cmd == "b":
		if *detail != nil {
			*detail = nil
		} else if *page > 0 {
			*page--
		}
	case strings.HasPrefix(cmd, "d "):
		rows := liveRows(active, ended)
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
			result := s.Run(ctx, "connections", "close", id)
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
		status = menuText(language, "menu.traffic.live.detail_ended", "Recently ended; end time and final counters are unavailable.")
	}
	c := row.connection
	fmt.Fprintf(out, "\n%s\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n[x] %s\n", status,
		menuText(language, "menu.traffic.live.id", "ID"), liveSafe(c.ID),
		menuText(language, "menu.traffic.live.target", "Target"), liveSafe(liveTarget(c)),
		menuText(language, "menu.traffic.live.process", "Process"), liveSafe(fmt.Sprint(c.Metadata["process"])),
		menuText(language, "menu.traffic.live.protocol", "Protocol"), liveSafe(fmt.Sprint(c.Metadata["network"])),
		menuText(language, "menu.traffic.live.proxy_chain", "Proxy chain"), liveSafe(strings.Join(c.Chains, " → ")),
		menuText(language, "menu.traffic.live.upload", "Upload"), humanBytes(c.Upload),
		menuText(language, "menu.traffic.live.download", "Download"), humanBytes(c.Download), menuText(language, "menu.traffic.live.back", "back to table"))
}

func liveRender(out io.Writer, language i18n.Context, rows []liveRow, filter, query, sortBy string, frozen bool, activeCount, endedCount int, sampled time.Time, input string, page int, detail *liveRow) {
	if term := os.Getenv("TERM"); term != "" && term != "dumb" && os.Getenv("NO_COLOR") == "" {
		fmt.Fprint(out, "\x1b[H\x1b[2J")
	} else {
		fmt.Fprintln(out)
	}
	fmt.Fprintln(out, menuText(language, "menu.traffic.live.instructions", "Connections — [f] filter  /text search  [s] sort  [p] freeze/resume  n/b page  d ID details  c ID close  x detail back  q return"))
	filterLabel := menuText(language, "menu.traffic.live.filter_all", "All")
	if filter == "active" {
		filterLabel = menuText(language, "menu.traffic.live.filter_active", "Active")
	}
	if filter == "ended" {
		filterLabel = menuText(language, "menu.traffic.live.filter_ended", "Recently ended")
	}
	fmt.Fprintf(out, "%s: %s  %s: %d  %s: %d  %s: %s", menuText(language, "menu.traffic.live.filter", "Filter"), filterLabel, menuText(language, "menu.traffic.live.active", "Active"), activeCount, menuText(language, "menu.traffic.live.ended", "Recently ended"), endedCount, menuText(language, "menu.traffic.live.sampled", "Sampled"), sampled.Format("15:04"))
	if frozen {
		fmt.Fprintf(out, "  %s", menuText(language, "menu.traffic.live.frozen", "Display frozen; collection continues."))
	}
	fmt.Fprintln(out)
	if detail != nil {
		liveDetail(out, language, *detail)
		fmt.Fprintf(out, "\n> %s", liveSafe(input))
		return
	}
	fmt.Fprintln(out, menuText(language, "menu.traffic.live.columns", "ID / TARGET + PROCESS     PROTOCOL  PROXY CHAIN        UP / DOWN          STATE"))
	filtered := liveFilteredRows(rows, filter, query, sortBy)

	pageSize := liveRowsPerPage()
	pages := livePageCount(filtered)
	if pages == 0 {
		pages = 1
	}
	if page >= pages {
		page = pages - 1
	}
	fmt.Fprintf(out, "%s %d/%d — n/b\n", menuText(language, "menu.traffic.live.page", "Page"), page+1, pages)
	start := page * pageSize
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	for _, r := range filtered[start:end] {
		c := r.connection
		state := menuText(language, "menu.traffic.live.active_state", "ACTIVE")
		if r.ended {
			state = menuText(language, "menu.traffic.live.ended_state", "ENDED")
		}
		fmt.Fprintf(out, "%s %s %s %s %s/%s %s\n", liveSafe(c.ID), liveFit(liveSafe(liveTarget(c)), 22), liveFit(liveSafe(fmt.Sprint(c.Metadata["network"])), 6), liveFit(liveSafe(strings.Join(c.Chains, " → ")), 14), liveFit(humanBytes(c.Upload), 8), liveFit(humanBytes(c.Download), 8), state)
	}
	fmt.Fprintf(out, "\n> %s", liveSafe(input))
}

func liveRowsPerPage() int {
	if n, err := strconv.Atoi(os.Getenv("LINES")); err == nil && n > 0 {
		n -= 7
		if n < 1 {
			return 1
		}
		if n > 30 {
			return 30
		}
		return n
	}
	return 12
}
func livePageCount(rows []liveRow) int {
	n := liveRowsPerPage()
	if len(rows) == 0 {
		return 0
	}
	return (len(rows) + n - 1) / n
}
func liveFilteredRows(rows []liveRow, filter, query, sortBy string) []liveRow {
	filtered := make([]liveRow, 0, len(rows))
	for _, r := range rows {
		if filter == "active" && r.ended || filter == "ended" && !r.ended {
			continue
		}
		text := strings.ToLower(fmt.Sprint(r.connection.ID, " ", r.connection.Metadata, " ", r.connection.Chains))
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
func liveFit(text string, width int) string {
	var b strings.Builder
	cells := 0
	for _, r := range text {
		cell := 1
		if r >= 0x1100 && (r <= 0x115f || r >= 0x2e80 && r <= 0xa4cf || r >= 0xac00 && r <= 0xd7a3 || r >= 0xf900 && r <= 0xfaff || r >= 0xfe10 && r <= 0xfe6f || r >= 0xff00 && r <= 0xff60) {
			cell = 2
		}
		if cells+cell > width {
			break
		}
		b.WriteRune(r)
		cells += cell
	}
	for cells < width {
		b.WriteByte(' ')
		cells++
	}
	return b.String()
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
