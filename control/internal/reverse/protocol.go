// Package reverse defines the hub-hostd reverse-routing transport frames.
package reverse

import "net/http"

const (
	FrameRequest       = "request"
	FrameRequestCancel = "request_cancel"
	FrameResponseStart = "response_start"
	FrameResponseBody  = "response_body"
	FrameResponseEnd   = "response_end"
	FrameResponseError = "response_error"
)

// MaxFrameBytes bounds one JSON websocket frame on the reverse transport. The
// default coder/websocket read limit is 32 KiB, which is too small for streamed
// session events containing tool results or transcripts.
const MaxFrameBytes int64 = 8 << 20

type Frame struct {
	Type      string      `json:"type"`
	RequestID string      `json:"request_id,omitempty"`
	Method    string      `json:"method,omitempty"`
	Path      string      `json:"path,omitempty"`
	Query     string      `json:"query,omitempty"`
	Body      []byte      `json:"body,omitempty"`
	Status    int         `json:"status,omitempty"`
	Headers   http.Header `json:"headers,omitempty"`
	Error     string      `json:"error,omitempty"`
}
