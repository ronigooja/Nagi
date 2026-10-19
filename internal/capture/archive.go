package capture

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

const ArchiveVersion = 1

var ErrArchive = errors.New("capture_archive_invalid")

type archiveLine struct {
	Type     string  `json:"type"`
	Version  int     `json:"version,omitempty"`
	Status   *Status `json:"status,omitempty"`
	Record   *Record `json:"record,omitempty"`
	RecordID string  `json:"record_id,omitempty"`
	Snapshot string  `json:"snapshot,omitempty"`
	Offset   int64   `json:"offset,omitempty"`
	Data     []byte  `json:"data,omitempty"`
	Records  int     `json:"records,omitempty"`
}

// Export streams a consistent native NDJSON or HAR archive. A concurrent session
// replacement/clear aborts the stream; the native end marker detects truncation.
func Export(ctx context.Context, socket string, w io.Writer, format string) error {
	if format != "native" && format != "har" {
		return errors.New("capture_format_invalid")
	}
	client := httpClient(socket)
	client.Timeout = 0
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://capture/export?format="+format, nil)
	if err != nil {
		return ErrUnavailable
	}
	resp, err := client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("capture_export_failed")
	}
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		return errors.New("capture_export_failed")
	}
	return nil
}

// Import accepts only the versioned native archive and atomically installs an
// offline, stopped session. Failed imports retain the previous session unchanged.
func Import(ctx context.Context, socket string, r io.Reader) (Status, error) {
	client := httpClient(socket)
	client.Timeout = 0
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://capture/import", r)
	if err != nil {
		return Status{}, ErrUnavailable
	}
	resp, err := client.Do(req)
	if err != nil {
		return Status{}, ErrUnavailable
	}
	defer resp.Body.Close()
	var reply Reply
	if json.NewDecoder(io.LimitReader(resp.Body, MaxChunk)).Decode(&reply) != nil {
		return Status{}, ErrArchive
	}
	if reply.Error != "" {
		return Status{}, errors.New(reply.Error)
	}
	if reply.Status == nil {
		return Status{}, ErrArchive
	}
	return *reply.Status, nil
}
func registerArchive(mux *http.ServeMux, s *Store, authorize func(http.ResponseWriter, *http.Request) bool, slots chan struct{}) {
	mux.HandleFunc("/export", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		defer func() { <-slots }()
		format := r.URL.Query().Get("format")
		if r.Method != http.MethodGet || (format != "native" && format != "har") {
			http.Error(w, "capture_format_invalid", 400)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		if format == "har" {
			w.Header().Set("Content-Type", "application/json")
		}
		var err error
		if format == "native" {
			err = s.exportNative(w)
		} else {
			err = s.exportHAR(w)
		}
		if err != nil {
			panic(http.ErrAbortHandler)
		}
	})
	mux.HandleFunc("/import", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		defer func() { <-slots }()
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			_ = json.NewEncoder(w).Encode(fail("capture_method_invalid"))
			return
		}
		status, err := s.importNative(http.MaxBytesReader(w, r.Body, MaxBytes*2))
		if err != nil {
			_ = json.NewEncoder(w).Encode(fail(err.Error()))
			return
		}
		_ = json.NewEncoder(w).Encode(Reply{Sensitive: true, Status: &status})
	})
}

type exportView struct {
	status Status
	epoch  string
	ids    []string
}

func (s *Store) view() exportView {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.current().Status
	return exportView{*st, s.epoch, append([]string{}, s.order...)}
}
func (s *Store) exportRecord(v exportView, id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epoch != v.epoch {
		return Record{}, ErrArchive
	}
	r, ok := s.records[id]
	if !ok {
		return Record{}, ErrArchive
	}
	return clone(r), nil
}
func (s *Store) exportChunk(v exportView, id, name string, off int64, n int) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epoch != v.epoch {
		return nil, ErrArchive
	}
	body := s.bodies[id][name]
	if off < 0 || off+int64(n) > body.length() {
		return nil, ErrArchive
	}
	return body.read(off, off+int64(n)), nil
}
func snapshotNames(r Record) []string {
	names := make([]string, 0, len(r.Snapshots))
	for n := range r.Snapshots {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
func (s *Store) exportNative(w io.Writer) error {
	v := s.view()
	enc := json.NewEncoder(w)
	if err := enc.Encode(archiveLine{Type: "session", Version: ArchiveVersion, Status: &v.status}); err != nil {
		return err
	}
	for _, id := range v.ids {
		r, err := s.exportRecord(v, id)
		if err != nil {
			return err
		}
		if err = enc.Encode(archiveLine{Type: "record", Record: &r}); err != nil {
			return err
		}
		for _, name := range snapshotNames(r) {
			snap := r.Snapshots[name]
			for off := int64(0); off < snap.Captured; {
				n := MaxChunk
				if int64(n) > snap.Captured-off {
					n = int(snap.Captured - off)
				}
				b, err := s.exportChunk(v, id, name, off, n)
				if err != nil {
					return err
				}
				if err = enc.Encode(archiveLine{Type: "body", RecordID: id, Snapshot: name, Offset: off, Data: b}); err != nil {
					return err
				}
				off += int64(n)
			}
		}
	}
	return enc.Encode(archiveLine{Type: "end", Records: len(v.ids)})
}
func (s *Store) importNative(reader io.Reader) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.State == "recording" || s.status.State == "paused" {
		return Status{}, errors.New("capture_state_invalid")
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), (3*DefaultBodyLimit)/2)
	next := NewStore()
	seenHeader, ended := false, false
	var expected map[string]Snapshot
	var active string
	finalize := func() error {
		if active == "" {
			return nil
		}
		for name, snap := range expected {
			if next.bodies[active][name].length() != snap.Captured {
				return ErrArchive
			}
		}
		return nil
	}
	for scanner.Scan() {
		if ended {
			return Status{}, ErrArchive
		}
		var line archiveLine
		dec := json.NewDecoder(strings.NewReader(scanner.Text()))
		dec.DisallowUnknownFields()
		if dec.Decode(&line) != nil {
			return Status{}, ErrArchive
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			return Status{}, ErrArchive
		}
		if !seenHeader {
			if line.Type != "session" || line.Version != ArchiveVersion || line.Status == nil {
				return Status{}, ErrArchive
			}
			st := *line.Status
			if st.SessionID == "" || !validID(st.SessionID) || st.Revision == 0 || st.BodyLimit < 1 || st.BodyLimit > MaxBodyLimit || size(st) > 64<<10 {
				return Status{}, ErrArchive
			}
			st.State = "stopped"
			st.ReadOnly = true
			st.Records = 0
			st.Bytes = 0
			next.status = clone(st)
			next.bytes = size(st)
			seenHeader = true
			continue
		}
		switch line.Type {
		case "record":
			if finalize() != nil || line.Record == nil || len(next.order) >= MaxRecords || size(line.Record) > 1<<20 {
				return Status{}, ErrArchive
			}
			r := clone(*line.Record)
			if r.ID == "" || !validID(r.ID) || r.SessionID != next.status.SessionID || r.Identity.SessionID != r.SessionID || r.Identity.Revision == 0 || r.Identity.Revision > next.status.Revision || (r.Kind != "http" && r.Kind != "websocket") {
				return Status{}, ErrArchive
			}
			if _, ok := next.records[r.ID]; ok {
				return Status{}, ErrArchive
			}
			if r.Kind == "websocket" {
				parent, ok := next.records[r.ParentID]
				if !ok || parent.Kind != "http" {
					return Status{}, ErrArchive
				}
			}
			for name, snap := range r.Snapshots {
				if !validSnapshot(name) || !validState(snap.BodyState) || snap.Captured < 0 || snap.BodySize < snap.Captured || snap.Captured > int64(next.status.BodyLimit) || snap.BodyState == "complete" && snap.Captured != snap.BodySize {
					return Status{}, ErrArchive
				}
				if snap.BodyState == "collecting" {
					snap.BodyState = "cutoff"
					r.Snapshots[name] = snap
				}
			}
			r.Error = fixedEventError(r.Error)
			next.records[r.ID] = r
			next.order = append(next.order, r.ID)
			next.bodies[r.ID] = map[string]*bodyData{}
			next.bytes += size(r)
			active = r.ID
			expected = r.Snapshots
		case "body":
			snap, ok := expected[line.Snapshot]
			if active == "" || line.RecordID != active || !ok || len(line.Data) == 0 || len(line.Data) > MaxChunk {
				return Status{}, ErrArchive
			}
			prev := next.bodies[active][line.Snapshot]
			if line.Offset != prev.length() || line.Offset+int64(len(line.Data)) > snap.Captured {
				return Status{}, ErrArchive
			}
			if prev == nil {
				prev = &bodyData{}
				next.bodies[active][line.Snapshot] = prev
			}
			growth := prev.growth(len(line.Data))
			prev.append(line.Data)
			next.bytes += growth
		case "end":
			if finalize() != nil || line.Records != len(next.order) {
				return Status{}, ErrArchive
			}
			ended = true
		default:
			return Status{}, ErrArchive
		}
		// Retaining the old session during transactional import shares the quota.
		if next.bytes+s.bytes > MaxBytes-8*DefaultBodyLimit {
			return Status{}, errors.New("capture_byte_limit")
		}
	}
	if scanner.Err() != nil || !seenHeader || !ended {
		return Status{}, ErrArchive
	}
	s.status = next.status
	s.records = next.records
	s.order = next.order
	s.positions = map[string]int{}
	for i, id := range s.order {
		s.positions[id] = i
	}
	s.nextPosition = len(s.order)
	s.bodies = next.bodies
	s.bytes = next.bytes
	s.epoch = newID()
	return *s.current().Status, nil
}

// HAR 1.2 retains complete capture metadata in _nagiRecord and binary original /
// actual snapshots in _nagiBodies. WebSocket messages live in the named log
// extension _nagiWebSocketMessages, with parent IDs linking HTTP handshakes.
func (s *Store) exportHAR(w io.Writer) error {
	v := s.view()
	if _, err := io.WriteString(w, `{"log":{"version":"1.2","creator":{"name":"Nagi","version":"1"},"entries":[`); err != nil {
		return err
	}
	first := true
	for _, id := range v.ids {
		r, err := s.exportRecord(v, id)
		if err != nil {
			return err
		}
		if r.Kind != "http" {
			continue
		}
		if !first {
			if _, err = io.WriteString(w, ","); err != nil {
				return err
			}
		}
		first = false
		if err = s.harEntry(w, v, r); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, `],"_nagiWebSocketMessages":[`); err != nil {
		return err
	}
	first = true
	for _, id := range v.ids {
		r, err := s.exportRecord(v, id)
		if err != nil {
			return err
		}
		if r.Kind != "websocket" {
			continue
		}
		if !first {
			if _, err = io.WriteString(w, ","); err != nil {
				return err
			}
		}
		first = false
		if err = s.harExtension(w, v, r); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, `]}}`)
	return err
}

type harHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func harHeaders(h map[string][]string) []harHeader {
	out := []harHeader{}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range h[k] {
			out = append(out, harHeader{k, v})
		}
	}
	return out
}
func (s *Store) harEntry(w io.Writer, v exportView, r Record) error {
	req := r.Snapshots["request_original"]
	res := r.Snapshots["response_original"]
	authority := req.Host
	if req.Port != 0 && !(req.Scheme == "https" && req.Port == 443) && !(req.Scheme == "http" && req.Port == 80) {
		authority += ":" + strconv.Itoa(req.Port)
	}
	url := req.Scheme + "://" + authority + req.Path
	if req.Query != "" {
		url += "?" + req.Query
	}
	entry := map[string]any{"startedDateTime": r.Started, "time": r.DurationMS, "request": map[string]any{"method": req.Method, "url": url, "httpVersion": "HTTP/1.1", "headers": harHeaders(req.Headers), "queryString": []any{}, "cookies": []any{}, "headersSize": -1, "bodySize": req.BodySize}, "response": map[string]any{"status": res.Status, "statusText": "", "httpVersion": "HTTP/1.1", "headers": harHeaders(res.Headers), "cookies": []any{}, "content": map[string]any{"size": res.BodySize, "mimeType": "application/octet-stream"}, "redirectURL": "", "headersSize": -1, "bodySize": res.BodySize}, "cache": map[string]any{}, "timings": map[string]int64{"send": 0, "wait": r.DurationMS, "receive": 0}}
	request := entry["request"].(map[string]any)
	response := entry["response"].(map[string]any)
	delete(entry, "request")
	delete(entry, "response")
	delete(response, "content")
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err = w.Write(data[:len(data)-1]); err != nil {
		return err
	}
	if _, err = io.WriteString(w, `,"request":`); err != nil {
		return err
	}
	data, err = json.Marshal(request)
	if err != nil {
		return err
	}
	if _, err = w.Write(data[:len(data)-1]); err != nil {
		return err
	}
	if _, err = io.WriteString(w, `,"postData":{"mimeType":"application/octet-stream","_encoding":"base64","text":"`); err != nil {
		return err
	}
	if err = s.harEncodedBody(w, v, r, "request_original"); err != nil {
		return err
	}
	if _, err = io.WriteString(w, `"}}`); err != nil {
		return err
	}
	if _, err = io.WriteString(w, `,"response":`); err != nil {
		return err
	}
	data, err = json.Marshal(response)
	if err != nil {
		return err
	}
	if _, err = w.Write(data[:len(data)-1]); err != nil {
		return err
	}
	if _, err = fmt.Fprintf(w, `,"content":{"size":%d,"mimeType":"application/octet-stream","encoding":"base64","text":"`, res.BodySize); err != nil {
		return err
	}
	if err = s.harEncodedBody(w, v, r, "response_original"); err != nil {
		return err
	}
	if _, err = io.WriteString(w, `"}}`); err != nil {
		return err
	}

	if _, err = io.WriteString(w, `,"_nagiRecord":`); err != nil {
		return err
	}
	if err = json.NewEncoder(w).Encode(r); err != nil {
		return err
	}
	if _, err = io.WriteString(w, `,"_nagiBodies":`); err != nil {
		return err
	}
	if err = s.harBodies(w, v, r); err != nil {
		return err
	}
	_, err = io.WriteString(w, "}")
	return err
}
func (s *Store) harExtension(w io.Writer, v exportView, r Record) error {
	if _, err := io.WriteString(w, `{"_nagiRecord":`); err != nil {
		return err
	}
	if err := json.NewEncoder(w).Encode(r); err != nil {
		return err
	}
	if _, err := io.WriteString(w, `,"_nagiBodies":`); err != nil {
		return err
	}
	if err := s.harBodies(w, v, r); err != nil {
		return err
	}
	_, err := io.WriteString(w, "}")
	return err
}
func (s *Store) harBodies(w io.Writer, v exportView, r Record) error {
	if _, err := io.WriteString(w, "{"); err != nil {
		return err
	}
	for i, name := range snapshotNames(r) {
		if i > 0 {
			if _, err := io.WriteString(w, ","); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "%q:", name); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `{"encoding":"base64","text":"`); err != nil {
			return err
		}
		if err := s.harEncodedBody(w, v, r, name); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `"}`); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "}")
	return err
}

func (s *Store) harEncodedBody(w io.Writer, v exportView, r Record, name string) error {
	encoder := base64.NewEncoder(base64.StdEncoding, w)
	snap := r.Snapshots[name]
	for off := int64(0); off < snap.Captured; {
		n := MaxChunk
		if int64(n) > snap.Captured-off {
			n = int(snap.Captured - off)
		}
		chunk, err := s.exportChunk(v, r.ID, name, off, n)
		if err != nil {
			return err
		}
		if _, err = encoder.Write(chunk); err != nil {
			return err
		}
		off += int64(n)
	}
	return encoder.Close()
}
