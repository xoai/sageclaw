// Package tunnel implements a native reverse tunnel for exposing
// SageClaw's webhook endpoints to the internet without external dependencies.
package tunnel

import (
	"encoding/json"
	"fmt"
)

// ProtocolVersion is the current tunnel protocol version.
const ProtocolVersion = 1

// Message types exchanged between tunnel client and relay.
const (
	TypeReady        = "ready"
	TypeRequest      = "request"
	TypeResponse     = "response"
	TypeRequestStart = "request_start"
	TypeRequestEnd   = "request_end"
	TypePing         = "ping"
	TypePong         = "pong"
	TypeError        = "error"
	TypeClose        = "close"
)

// MaxInlineBody is the threshold for inline vs chunked body transfer.
// Bodies smaller than this are included directly in the JSON message.
const MaxInlineBody = 1 << 20 // 1MB

// MaxBodySize is the absolute maximum body size allowed.
const MaxBodySize = 10 << 20 // 10MB

// ChunkSize is the size of binary WebSocket frames for chunked transfers.
const ChunkSize = 64 << 10 // 64KB

// MaxConcurrentRequests is the per-tunnel request concurrency limit.
const MaxConcurrentRequests = 50

// Message is the envelope for all tunnel protocol messages.
// Only the fields relevant to a given Type are populated.
type Message struct {
	Type string `json:"type"`

	// ready
	URL       string `json:"url,omitempty"`
	Subdomain string `json:"subdomain,omitempty"`

	// request / response
	ID      string            `json:"id,omitempty"`
	Method  string            `json:"method,omitempty"`
	Path    string            `json:"path,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"body,omitempty"`    // inline body (< 1MB, text)
	BodyB64 string            `json:"body_b64,omitempty"` // inline body (< 1MB, binary as base64)
	Status  int               `json:"status,omitempty"`

	// request_start / request_end (chunked mode)
	// request_start carries ID, Method, Path, Headers (no body)
	// request_end carries only ID
	// Binary frames between start/end carry raw body chunks

	// ping / pong
	Timestamp int64 `json:"timestamp,omitempty"`

	// error
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`

	// close
	Reason string `json:"reason,omitempty"`
}

// Encode serializes a Message to JSON.
func (m *Message) Encode() ([]byte, error) {
	return json.Marshal(m)
}

// DecodeMessage deserializes a JSON message.
func DecodeMessage(data []byte) (*Message, error) {
	var m Message
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("decode tunnel message: %w", err)
	}
	if m.Type == "" {
		return nil, fmt.Errorf("decode tunnel message: missing type field")
	}
	return &m, nil
}
