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
