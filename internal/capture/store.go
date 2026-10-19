package capture

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
	"golang.org/x/net/publicsuffix"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// Store retains a single session in bounded memory. It never persists traffic.
// Eight MiB are reserved for ingress, archive, and transport buffers outside storage.
type Store struct {
	mu           sync.Mutex
	status       Status
	records      map[string]Record
	order        []string
	bodies       map[string]map[string]*bodyData
	epoch        string
	bytes        int64
	positions    map[string]int
	nextPosition int
}

func NewStore() *Store {
	return &Store{status: Status{State: "stopped"}, records: map[string]Record{}, bodies: map[string]map[string]*bodyData{}, epoch: newID(), positions: map[string]int{}}
}
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("capture entropy unavailable")
	}
	return hex.EncodeToString(b[:])
}
func clone[T any](v T) T     { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }
func size(v any) int64       { b, _ := json.Marshal(v); return int64(len(b)) }
func fail(code string) Reply { return Reply{Sensitive: true, Error: code} }
func validSnapshot(s string) bool {
	switch s {
	case "request_original", "request_actual", "response_original", "response_actual", "message_original", "message_actual":
		return true
	}
	return false
}
func validState(s string) bool {
	switch s {
	case "collecting", "complete", "truncated", "failed", "cutoff", "unavailable", "not_collected":
		return true
	}
	return false
}
func (s *Store) current() Reply {
	st := clone(s.status)
	st.Records = len(s.records)
	st.Bytes = s.bytes
	return Reply{Sensitive: true, Status: &st}
}

// Handle validates and applies one operation. Returned data does not alias storage.
func (s *Store) Handle(q Request) Reply { s.mu.Lock(); defer s.mu.Unlock(); return s.handle(q) }
func (s *Store) handle(q Request) Reply {
	switch q.Action {
	case "status":
		return s.current()
	case "start":
		if q.Start == nil || s.status.State == "recording" || s.status.State == "paused" {
			return fail("capture_start_invalid")
		}
		o := clone(*q.Start)
		if !normalizeScope(&o.Scope) {
			return fail("capture_scope_invalid")
		}
		if o.BodyLimit == 0 {
			o.BodyLimit = DefaultBodyLimit
		}
		if o.BodyLimit < 1 || o.BodyLimit > MaxBodyLimit || size(o) > MaxChunk {
			return fail("capture_start_invalid")
		}
		if o.SessionID == "" {
			o.SessionID = newID()
		}
		if !validID(o.SessionID) {
			return fail("capture_start_invalid")
		}
		s.status = Status{SessionID: o.SessionID, Revision: 1, State: "recording", Profile: o.Profile, Backend: o.Backend, Owner: o.Owner, Scope: o.Scope, BodyLimit: o.BodyLimit}
		s.records = map[string]Record{}
		s.bodies = map[string]map[string]*bodyData{}
		s.order = nil
		s.positions = map[string]int{}
		s.nextPosition = 0
		s.epoch = newID()
		s.bytes = size(s.status)
		return s.current()
	case "list":
		return s.list(q.List)
	case "show":
		r, ok := s.records[q.RecordID]
		if !ok {
			return fail("capture_record_missing")
		}
		r = clone(r)
		return Reply{Sensitive: true, Record: &r}
	case "body":
		return s.body(q)
	}
	if q.SessionID == "" || q.SessionID != s.status.SessionID {
		return fail("capture_session_mismatch")
	}
	if s.status.ReadOnly {
		return fail("capture_read_only")
	}
	switch q.Action {
	case "pause", "resume", "stop":
		next := map[string]string{"pause": "paused", "resume": "recording", "stop": "stopped"}[q.Action]
		if s.status.State == next {
			return s.current()
		}
		if s.status.State == "stopped" {
			return fail("capture_state_invalid")
		}
		s.status.State = next
		s.status.Revision++
		if next == "stopped" || next == "paused" {
			for id, r := range s.records {
				for name, snap := range r.Snapshots {
					if snap.BodyState == "collecting" {
						snap.BodyState = "cutoff"
						r.Snapshots[name] = snap
					}
				}
				s.records[id] = r
			}
		}
		return s.current()
	case "clear":
		s.records = map[string]Record{}
		s.bodies = map[string]map[string]*bodyData{}
		s.order = nil
		s.positions = map[string]int{}
		s.nextPosition = 0
		s.bytes = size(s.status)
		s.epoch = newID()
		return s.current()
	case "event":
		if q.Event == nil {
			return fail("capture_event_invalid")
		}
		if s.status.State != "recording" {
			return fail("capture_not_recording")
		}
		return s.event(*q.Event)
	default:
		return fail("capture_action_invalid")
	}
}
func (s *Store) event(e Event) Reply {
	if e.SessionID != s.status.SessionID || e.RecordID == "" || !validID(e.RecordID) {
		return fail("capture_event_invalid")
	}
	r, exists := s.records[e.RecordID]
	oldSize := size(r)
	if !exists {
		oldSize = 0
	}
	switch e.Type {
	case "record":
		if e.Record == nil || len(e.Data) > 0 {
			return fail("capture_event_invalid")
		}
		if size(e.Record) > 1<<20 {
			return fail("capture_metadata_limit")
		}
		n := clone(*e.Record)
		n.Error = fixedEventError(n.Error)
		if n.ID != e.RecordID || n.SessionID != s.status.SessionID || (n.Kind != "http" && n.Kind != "websocket") || n.Identity.SessionID != s.status.SessionID || n.Identity.Revision == 0 || n.Identity.Revision != s.status.Revision {
			return fail("capture_event_invalid")
		}
		if n.Kind == "websocket" {
			p, ok := s.records[n.ParentID]
			if !ok || p.Kind != "http" {
				return fail("capture_parent_missing")
			}
		}
		for k, v := range n.Snapshots {
			if !validSnapshot(k) || !validState(v.BodyState) || v.Captured != 0 || v.BodySize < 0 || v.BodyState == "complete" && v.BodySize != 0 {
				return fail("capture_event_invalid")
			}
		}
		if exists {
			for k, v := range r.Snapshots {
				if body := s.bodies[r.ID][k]; body.length() > 0 || v.BodyState != "collecting" {
					n.Snapshots[k] = v
				}
			}
		} else if len(s.records) >= MaxRecords && !s.evict(e.Record.ParentID) {
			s.status.Dropped++
			return fail("capture_record_limit")
		}
		r = n
	case "body":
		if !exists || !validSnapshot(e.Snapshot) || len(e.Data) > MaxChunk || e.Offset < 0 {
			return fail("capture_event_invalid")
		}
		snap, ok := r.Snapshots[e.Snapshot]
		if !ok || snap.BodyState != "collecting" && snap.BodyState != "truncated" {
			return fail("capture_snapshot_closed")
		}
		if e.Offset < snap.BodySize {
			return fail("capture_offset_invalid")
		}
		r = clone(r)
		gap := e.Offset != snap.BodySize
		allowed := s.status.BodyLimit - int(snap.Captured)
		if allowed < 0 || gap || snap.BodyState == "truncated" {
			allowed = 0
		}
		if allowed > len(e.Data) {
			allowed = len(e.Data)
		}
		for s.bytes+int64(allowed)+256 > MaxBytes-8*DefaultBodyLimit && s.evict(r.ID) {
		}
		remaining := int64(MaxBytes-8*DefaultBodyLimit) - s.bytes - 256
		if int64(allowed) > remaining {
			allowed = int(remaining)
			if allowed < 0 {
				allowed = 0
			}
		}
		snap.BodySize = e.Offset + int64(len(e.Data))
		snap.Captured += int64(allowed)
		if allowed < len(e.Data) {
			if snap.BodyState != "truncated" {
				s.status.Truncated++
			}
			snap.BodyState = "truncated"
		}
		r.Snapshots[e.Snapshot] = snap
		if s.bodies[r.ID] == nil {
			s.bodies[r.ID] = map[string]*bodyData{}
		}
		if s.bodies[r.ID][e.Snapshot] == nil {
			s.bodies[r.ID][e.Snapshot] = &bodyData{}
		}
		s.bodies[r.ID][e.Snapshot].append(e.Data[:allowed])
		s.bytes += int64(allowed)

	case "finish":
		if size(e.Trailers) > 256<<10 {
			return fail("capture_metadata_limit")
		}
		if !exists || !validSnapshot(e.Snapshot) || !validState(e.State) || e.State == "collecting" {
			return fail("capture_event_invalid")
		}
		r = clone(r)
		snap, ok := r.Snapshots[e.Snapshot]
		if !ok {
			return fail("capture_snapshot_missing")
		}
		if e.BodySize < 0 {
			return fail("capture_event_invalid")
		}
		if snap.BodyState != "truncated" && snap.BodyState != "cutoff" {
			snap.BodyState = e.State
		}
		if e.State == "complete" && (e.BodySize != snap.BodySize || snap.Captured != e.BodySize) && snap.BodyState != "cutoff" {
			if snap.BodyState != "truncated" {
				s.status.Truncated++
			}
			snap.BodyState = "truncated"
		}
		if e.BodySize > snap.BodySize {
			snap.BodySize = e.BodySize
		}
		snap.Trailers = clone(e.Trailers)
		r.Snapshots[e.Snapshot] = snap
	case "complete":
		if !exists || len(e.Outcome) > 64 || len(e.Error) > 64 {
			return fail("capture_event_invalid")
		}
		r = clone(r)
		r.Outcome = e.Outcome
		r.Error = fixedEventError(e.Error)
		r.DurationMS = e.DurationMS
		r.CloseCode = e.CloseCode
	default:
		return fail("capture_event_invalid")
	}
	delta := size(r) - oldSize
	protect := r.ID
	if !exists && r.ParentID != "" {
		protect = r.ParentID
	}
	for s.bytes+delta > MaxBytes-8*DefaultBodyLimit && s.evict(protect) {
	}
	if s.bytes+delta > MaxBytes-8*DefaultBodyLimit {
		s.status.Dropped++
		return fail("capture_byte_limit")
	}
	s.bytes += delta
	s.records[r.ID] = r
	if !exists {
		s.positions[r.ID] = s.nextPosition
		s.nextPosition++
		s.order = append(s.order, r.ID)
	}
	return s.current()
}
func fixedEventError(code string) string {
	switch code {
	case "":
		return ""
	case "capture_failed", "request_failed", "response_failed", "websocket_failed", "body_failed", "stream_failed", "timeout", "canceled":
		return code
	default:
		return "capture_failed"
	}
}
func (s *Store) body(q Request) Reply {
	r, ok := s.records[q.RecordID]
	if !ok {
		return fail("capture_record_missing")
	}
	snap, ok := r.Snapshots[q.Snapshot]
	if !ok {
		return fail("capture_snapshot_missing")
	}
	b := s.bodies[q.RecordID][q.Snapshot]
	if q.Offset < 0 || q.Offset > b.length() || q.Limit < 0 || q.Limit > MaxChunk {
		return fail("capture_range_invalid")
	}
	limit := q.Limit
	if limit == 0 {
		limit = MaxChunk
	}
	end := q.Offset + int64(limit)
	if end > b.length() {
		end = b.length()
	}
	chunk := &BodyChunk{Data: b.read(q.Offset, end), Offset: q.Offset, NextOffset: end, EOF: end == b.length(), State: snap.BodyState}
	return Reply{Sensitive: true, Body: chunk}
}
func (s *Store) list(opts *ListOptions) Reply {
	var o ListOptions
	if opts != nil {
		o = *opts
	}
	if o.Limit == 0 {
		o.Limit = 100
	}
	if o.Limit < 1 || o.Limit > 200 || len(o.Query) > 4096 {
		return fail("capture_list_invalid")
	}
	start := 0
	if o.Cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(o.Cursor)
		parts := strings.Split(string(b), ":")
		if err != nil || len(parts) != 2 || parts[0] != s.epoch {
			return fail("capture_cursor_invalid")
		}
		start, err = strconv.Atoi(parts[1])
		if err != nil || start < 0 || start > s.nextPosition {
			return fail("capture_cursor_invalid")
		}
	}
	out := Reply{Sensitive: true, Records: []Record{}}
	pageBytes := int64(0)
	i := 0
	for ; i < len(s.order); i++ {
		if s.positions[s.order[i]] < start {
			continue
		}
		r := s.records[s.order[i]]
		if o.Kind != "" && r.Kind != o.Kind || o.Stage != "" && r.Stage != o.Stage || o.ProcessPath != "" && r.Identity.ProcessPath != o.ProcessPath {
			continue
		}
		if o.Domain != "" {
			matched := false
			for _, snap := range r.Snapshots {
				if strings.EqualFold(snap.Host, o.Domain) {
					matched = true
				}
			}
			if !matched {
				continue
			}
		}
		if o.Query != "" {
			b, _ := json.Marshal(r)
			matched := strings.Contains(strings.ToLower(string(b)), strings.ToLower(o.Query))
			if !matched {
				for _, body := range s.bodies[r.ID] {
					if body.contains(o.Query) {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
		}
		recordBytes := size(r)
		if pageBytes+recordBytes > 1<<20 && len(out.Records) > 0 {
			break
		}
		pageBytes += recordBytes
		out.Records = append(out.Records, clone(r))
		if len(out.Records) == o.Limit {
			i++
			break
		}
	}
	if i < len(s.order) {
		out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(s.epoch + ":" + strconv.Itoa(s.positions[s.order[i]])))
	}
	return out
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
func completeRecord(r Record) bool {
	if r.Outcome != "" {
		return true
	}
	if len(r.Snapshots) == 0 {
		return false
	}
	for _, snap := range r.Snapshots {
		if snap.BodyState == "collecting" {
			return false
		}
	}
	return true
}

// evict removes the oldest completed record and its completed message children.
// An active handshake/message group and the incoming message's parent survive.
func (s *Store) evict(protect string) bool {
	for _, id := range s.order {
		r := s.records[id]
		if id == protect || !completeRecord(r) {
			continue
		}
		remove := map[string]bool{id: true}
		safe := true
		for childID, child := range s.records {
			if child.ParentID == id {
				if !completeRecord(child) || childID == protect {
					safe = false
					break
				}
				remove[childID] = true
			}
		}
		if !safe {
			continue
		}
		order := s.order[:0]
		for _, candidate := range s.order {
			if !remove[candidate] {
				order = append(order, candidate)
				continue
			}
			s.bytes -= size(s.records[candidate])
			for _, b := range s.bodies[candidate] {
				s.bytes -= b.length()
			}
			delete(s.records, candidate)
			delete(s.bodies, candidate)
			delete(s.positions, candidate)
			s.status.Dropped++
		}
		s.order = order
		return true
	}
	return false
}

// Chunk storage avoids allocating another multi-MiB copy on every body append.
type bodyData struct {
	chunks [][]byte
	n      int64
}

func (b *bodyData) length() int64 {
	if b == nil {
		return 0
	}
	return b.n
}
func (b *bodyData) append(data []byte) {
	if len(data) == 0 {
		return
	}
	b.chunks = append(b.chunks, append([]byte{}, data...))
	b.n += int64(len(data))
}
func (b *bodyData) read(start, end int64) []byte {
	out := make([]byte, end-start)
	if b == nil {
		return out
	}
	pos := int64(0)
	written := 0
	for _, chunk := range b.chunks {
		next := pos + int64(len(chunk))
		if next > start && pos < end {
			lo, hi := int64(0), int64(len(chunk))
			if start > pos {
				lo = start - pos
			}
			if end < next {
				hi = end - pos
			}
			written += copy(out[written:], chunk[lo:hi])
		}
		pos = next
		if pos >= end {
			break
		}
	}
	return out
}
func (b *bodyData) contains(query string) bool {
	if b == nil {
		return false
	}
	needle := []byte(strings.ToLower(query))
	var tail []byte
	for _, chunk := range b.chunks {
		combined := append(tail, []byte(strings.ToLower(string(chunk)))...)
		if bytes.Contains(combined, needle) {
			return true
		}
		keep := len(needle) - 1
		if keep > len(combined) {
			keep = len(combined)
		}
		tail = append([]byte{}, combined[len(combined)-keep:]...)
	}
	return false
}
func normalizeScope(scope *Scope) bool {
	if len(scope.Domains) > 256 || len(scope.ProcessPaths) > 256 || len(scope.AppPaths) > 256 {
		return false
	}
	seen := map[string]bool{}
	for i, domain := range scope.Domains {
		wild := strings.HasPrefix(domain, "*.")
		base := strings.TrimPrefix(domain, "*.")
		d, e := rewrite.NormalizeDomain(base)
		if e != nil {
			return false
		}
		if wild {
			suffix, _ := publicsuffix.PublicSuffix(d)
			if suffix == d || !strings.Contains(d, ".") {
				return false
			}
			d = "*." + d
		}
		if seen[d] {
			return false
		}
		seen[d] = true
		scope.Domains[i] = d
	}
	for _, paths := range [][]string{scope.ProcessPaths, scope.AppPaths} {
		seen := map[string]bool{}
		for _, p := range paths {
			if !filepath.IsAbs(p) || filepath.Clean(p) != p || len(p) > 4096 || strings.IndexFunc(p, unicode.IsControl) >= 0 || seen[p] {
				return false
			}
			seen[p] = true
		}
	}
	for _, p := range scope.AppPaths {
		if !strings.HasSuffix(p, ".app") || strings.Contains(p, ".app/") {
			return false
		}
	}
	return true
}
