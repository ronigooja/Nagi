// Package capture owns bounded, explicitly authorized original traffic records.
package capture

import "time"

const (
	ProtocolVersion  = 1
	MaxRecords       = 1000
	MaxBytes         = 64 << 20
	DefaultBodyLimit = 1 << 20
	MaxBodyLimit     = 8 << 20
	MaxChunk         = 256 << 10
)

type Scope struct {
	Domains      []string `json:"domains"`
	ProcessPaths []string `json:"process_paths"`
	AppPaths     []string `json:"app_paths"`
}
type Policy struct {
	SessionID string `json:"session_id"`
	Revision  uint64 `json:"revision"`
	State     string `json:"state"`
	Scope     Scope  `json:"scope"`
}
type Identity struct {
	SessionID   string `json:"session_id"`
	Revision    uint64 `json:"revision"`
	UID         uint32 `json:"uid"`
	Process     string `json:"process"`
	ProcessPath string `json:"process_path"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
}
type StartOptions struct {
	SessionID string `json:"session_id,omitempty"`
	Profile   string `json:"profile"`
	Backend   string `json:"backend"`
	Owner     string `json:"owner"`
	Scope     Scope  `json:"scope"`
	BodyLimit int    `json:"body_limit,omitempty"`
}
type Status struct {
	RecorderState string `json:"recorder_state,omitempty"`
	Recovery      string `json:"recovery,omitempty"`
	SessionID     string `json:"session_id"`
	Revision      uint64 `json:"revision"`
	State         string `json:"state"`
	Profile       string `json:"profile"`
	Backend       string `json:"backend"`
	Owner         string `json:"owner"`
	Scope         Scope  `json:"scope"`
	BodyLimit     int    `json:"body_limit"`
	Records       int    `json:"records"`
	Bytes         int64  `json:"bytes"`
	Dropped       uint64 `json:"dropped"`
	Truncated     uint64 `json:"truncated"`
	ReadOnly      bool   `json:"read_only"`
}
type Snapshot struct {
	Scheme      string              `json:"scheme,omitempty"`
	Host        string              `json:"host,omitempty"`
	Port        int                 `json:"port,omitempty"`
	Method      string              `json:"method,omitempty"`
	Path        string              `json:"path,omitempty"`
	Query       string              `json:"query,omitempty"`
	Status      int                 `json:"status,omitempty"`
	MessageType string              `json:"message_type,omitempty"`
	Headers     map[string][]string `json:"headers"`
	Trailers    map[string][]string `json:"trailers,omitempty"`
	BodyState   string              `json:"body_state"`
	BodySize    int64               `json:"body_size"`
	Captured    int64               `json:"captured"`
}
type Record struct {
	ID          string              `json:"id"`
	SessionID   string              `json:"session_id"`
	ParentID    string              `json:"parent_id,omitempty"`
	Kind        string              `json:"kind"`
	Stage       string              `json:"stage,omitempty"`
	Sequence    uint64              `json:"sequence,omitempty"`
	Generation  string              `json:"generation"`
	RuleVersion string              `json:"rule_version"`
	Identity    Identity            `json:"identity"`
	Started     time.Time           `json:"started"`
	DurationMS  int64               `json:"duration_ms"`
	RuleIDs     []string            `json:"rule_ids"`
	Outcome     string              `json:"outcome"`
	Error       string              `json:"error,omitempty"`
	CloseCode   int                 `json:"close_code,omitempty"`
	Snapshots   map[string]Snapshot `json:"snapshots"`
}
type Event struct {
	Type       string              `json:"type"`
	SessionID  string              `json:"session_id"`
	RecordID   string              `json:"record_id"`
	Snapshot   string              `json:"snapshot,omitempty"`
	Record     *Record             `json:"record,omitempty"`
	Data       []byte              `json:"data,omitempty"`
	Offset     int64               `json:"offset,omitempty"`
	BodySize   int64               `json:"body_size,omitempty"`
	State      string              `json:"state,omitempty"`
	Trailers   map[string][]string `json:"trailers,omitempty"`
	Outcome    string              `json:"outcome,omitempty"`
	Error      string              `json:"error,omitempty"`
	DurationMS int64               `json:"duration_ms,omitempty"`
	CloseCode  int                 `json:"close_code,omitempty"`
}
type ListOptions struct {
	Cursor      string `json:"cursor,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	Query       string `json:"query,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Stage       string `json:"stage,omitempty"`
	ProcessPath string `json:"process_path,omitempty"`
	Domain      string `json:"domain,omitempty"`
}
type BodyChunk struct {
	Data       []byte `json:"data"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	EOF        bool   `json:"eof"`
	State      string `json:"state"`
}
type Request struct {
	Action    string        `json:"action"`
	SessionID string        `json:"session_id,omitempty"`
	Start     *StartOptions `json:"start,omitempty"`
	List      *ListOptions  `json:"list,omitempty"`
	RecordID  string        `json:"record_id,omitempty"`
	Snapshot  string        `json:"snapshot,omitempty"`
	Offset    int64         `json:"offset,omitempty"`
	Limit     int           `json:"limit,omitempty"`
	Event     *Event        `json:"event,omitempty"`
}

// Readiness reports only the first unmet prerequisite, without starting a recorder.
type Readiness struct {
	Ready   bool   `json:"ready"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

type Reply struct {
	Readiness  *Readiness `json:"readiness,omitempty"`
	Sensitive  bool       `json:"sensitive"`
	Status     *Status    `json:"status,omitempty"`
	Records    []Record   `json:"records,omitempty"`
	Record     *Record    `json:"record,omitempty"`
	Body       *BodyChunk `json:"body,omitempty"`
	NextCursor string     `json:"next_cursor,omitempty"`
	Error      string     `json:"error,omitempty"`
}
