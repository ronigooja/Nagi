// Package connectionevents defines the versioned wire contract for mihomo
// connection failure events. The engine owns event production; Nagi owns
// consuming and presenting these messages.
package connectionevents

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	// ProtocolVersion is the version of the connection-events message format.
	ProtocolVersion = 1

	// DefaultQueueCapacity bounds the retained event history in a mihomo
	// process. A future engine implementation may expose this as read-only
	// hello metadata, but must not make it client-configurable.
	DefaultQueueCapacity = 1024
)

// Kind identifies a connection-events stream message.
type Kind string

const (
	KindHello            Kind = "hello"
	KindConnectionFailed Kind = "connection_failure"
	KindGap              Kind = "gap"
)

// Stage identifies the broad operation that failed. It is deliberately
// smaller than mihomo's internal call graph so it remains stable across
// protocol and adapter implementations.
type Stage string

const (
	StageResolve   Stage = "resolve"
	StageRoute     Stage = "route"
	StageDial      Stage = "dial"
	StageHandshake Stage = "handshake"
	StageForward   Stage = "forward"
)

// FailureCode is a stable machine-readable classification. The original
// error text is not part of the contract and must not be used for branching.
type FailureCode string

const (
	FailureDNS         FailureCode = "dns_failed"
	FailureRoute       FailureCode = "route_failed"
	FailureDial        FailureCode = "proxy_dial_failed"
	FailureHandshake   FailureCode = "proxy_handshake_failed"
	FailureTimeout     FailureCode = "connect_timeout"
	FailureRefused     FailureCode = "connection_refused"
	FailureUnreachable FailureCode = "network_unreachable"
	FailureTLS         FailureCode = "tls_failed"
	FailureCanceled    FailureCode = "canceled"
	FailureUnknown     FailureCode = "unknown"
)

// ConnectionMetadata contains only routing and endpoint metadata. Sensitive
// configuration values, credentials and raw error strings are not included.
// Fields may be absent when a failure occurs before metadata is complete.
type ConnectionMetadata struct {
	ConnectionID    string   `json:"connection_id,omitempty"`
	Network         string   `json:"network,omitempty"`
	Source          string   `json:"source,omitempty"`
	Destination     string   `json:"destination,omitempty"`
	DestinationHost string   `json:"destination_host,omitempty"`
	DestinationPort uint16   `json:"destination_port,omitempty"`
	Rule            string   `json:"rule,omitempty"`
	RulePayload     string   `json:"rule_payload,omitempty"`
	Proxy           string   `json:"proxy,omitempty"`
	Chains          []string `json:"chains,omitempty"`
}

// Message is the single JSON object sent on the connection-events WebSocket.
// The fields used depend on kind:
//   - hello uses stream_id, capacity, oldest_sequence and latest_sequence;
//   - connection_failure uses event_id, sequence, timestamp, stage, code and
//     metadata;
//   - gap uses from_sequence, to_sequence, dropped and reason.
type Message struct {
	Version int  `json:"version"`
	Kind    Kind `json:"kind"`

	StreamID       string `json:"stream_id,omitempty"`
	Capacity       int    `json:"capacity,omitempty"`
	OldestSequence uint64 `json:"oldest_sequence,omitempty"`
	LatestSequence uint64 `json:"latest_sequence,omitempty"`

	EventID   string              `json:"event_id,omitempty"`
	Sequence  uint64              `json:"sequence,omitempty"`
	Timestamp time.Time           `json:"timestamp,omitempty"`
	Stage     Stage               `json:"stage,omitempty"`
	Code      FailureCode         `json:"code,omitempty"`
	Metadata  *ConnectionMetadata `json:"metadata,omitempty"`

	FromSequence uint64 `json:"from_sequence,omitempty"`
	ToSequence   uint64 `json:"to_sequence,omitempty"`
	Dropped      uint64 `json:"dropped,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// NewHello returns the initial stream message. A client must accept hello
// before processing events; it identifies the process-lifetime sequence
// namespace and the bounded history available to that stream.
func NewHello(streamID string, capacity int, oldest, latest uint64) Message {
	return Message{
		Version:        ProtocolVersion,
		Kind:           KindHello,
		StreamID:       streamID,
		Capacity:       capacity,
		OldestSequence: oldest,
		LatestSequence: latest,
	}
}

// NewStreamID creates an opaque process-lifetime identifier. Event IDs are
// streamID/sequence, so IDs remain unique across engine restarts without
// exposing wall-clock or host information.
func NewStreamID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate connection event stream ID: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// EventLog is a bounded, concurrency-safe event history. Publishing never
// blocks: when full, the oldest events are discarded. Readers receive an
// explicit gap message before retained events whenever their cursor is older
// than the retained history.
type EventLog struct {
	mu       sync.RWMutex
	streamID string
	capacity int
	next     uint64
	events   []Message
}

// NewEventLog creates a bounded event history. A non-positive capacity is
// rejected because an unbounded or zero-retention stream cannot provide
// reliable gap semantics.
func NewEventLog(streamID string, capacity int) (*EventLog, error) {
	if streamID == "" {
		return nil, errors.New("connection event stream ID is empty")
	}
	if capacity <= 0 {
		return nil, errors.New("connection event queue capacity must be positive")
	}
	return &EventLog{streamID: streamID, capacity: capacity}, nil
}

// StreamID returns the opaque process-lifetime namespace for event IDs.
func (l *EventLog) StreamID() string { return l.streamID }

// Capacity returns the fixed retained event count.
func (l *EventLog) Capacity() int { return l.capacity }

// Publish appends one failure event and returns the assigned wire message.
// Zero timestamps are replaced with the current UTC time at publication.
func (l *EventLog) Publish(timestamp time.Time, stage Stage, code FailureCode, metadata *ConnectionMetadata) Message {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.next++
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	} else {
		timestamp = timestamp.UTC()
	}
	event := Message{
		Version:   ProtocolVersion,
		Kind:      KindConnectionFailed,
		EventID:   fmt.Sprintf("%s/%d", l.streamID, l.next),
		Sequence:  l.next,
		Timestamp: timestamp,
		Stage:     stage,
		Code:      code,
		Metadata:  cloneMetadata(metadata),
	}
	l.events = append(l.events, event)
	if len(l.events) > l.capacity {
		l.events = l.events[len(l.events)-l.capacity:]
	}
	return cloneMessage(event)
}

// Hello returns the current stream metadata. Sequences are one-based; zero
// means that no failure event has been published yet.
func (l *EventLog) Hello() Message {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return NewHello(l.streamID, l.capacity, l.oldestLocked(), l.next)
}

// ReadAfter returns at most limit messages after sequence. If retained
// history starts after sequence+1, the first message is a synthetic overflow
// gap. A limit must be positive; the caller should retry with a larger limit
// after consuming a gap if it needs the whole retained batch.
func (l *EventLog) ReadAfter(sequence uint64, limit int) ([]Message, error) {
	if limit <= 0 {
		return nil, errors.New("connection event read limit must be positive")
	}

	l.mu.RLock()
	defer l.mu.RUnlock()
	if sequence >= l.next {
		return nil, nil
	}

	result := make([]Message, 0, limit)
	oldest := l.oldestLocked()
	if oldest > 0 && sequence+1 < oldest {
		gap := Message{
			Version:      ProtocolVersion,
			Kind:         KindGap,
			FromSequence: sequence + 1,
			ToSequence:   oldest - 1,
			Dropped:      oldest - (sequence + 1),
			Reason:       "overflow",
		}
		result = append(result, gap)
		sequence = oldest - 1
	}
	for _, event := range l.events {
		if event.Sequence <= sequence {
			continue
		}
		if len(result) == limit {
			break
		}
		result = append(result, cloneMessage(event))
	}
	return result, nil
}

func (l *EventLog) oldestLocked() uint64 {
	if len(l.events) == 0 {
		return 0
	}
	return l.events[0].Sequence
}

func cloneMessage(message Message) Message {
	message.Metadata = cloneMetadata(message.Metadata)
	return message
}

func cloneMetadata(metadata *ConnectionMetadata) *ConnectionMetadata {
	if metadata == nil {
		return nil
	}
	clone := *metadata
	clone.Chains = append([]string(nil), metadata.Chains...)
	return &clone
}

// DecodeMessage validates the discriminant and protocol version before
// returning a message. Unknown fields are ignored for forward compatibility.
func DecodeMessage(data []byte) (Message, error) {
	var message Message
	if err := json.Unmarshal(data, &message); err != nil {
		return Message{}, fmt.Errorf("decode connection event message: %w", err)
	}
	if message.Version != ProtocolVersion {
		return Message{}, fmt.Errorf("unsupported connection event protocol version %d", message.Version)
	}
	switch message.Kind {
	case KindHello, KindConnectionFailed, KindGap:
		return message, nil
	default:
		return Message{}, fmt.Errorf("unknown connection event kind %q", message.Kind)
	}
}
