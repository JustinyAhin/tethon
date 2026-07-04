package reverse

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func ServeRequests(ctx context.Context, conn *websocket.Conn, handler http.Handler) error {
	var writeMu sync.Mutex
	active := make(map[string]context.CancelFunc)
	var activeMu sync.Mutex
	defer func() {
		activeMu.Lock()
		for _, cancel := range active {
			cancel()
		}
		activeMu.Unlock()
	}()

	for {
		var frame Frame
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			return err
		}
		switch frame.Type {
		case FrameRequest:
			reqCtx, cancel := context.WithCancel(ctx)
			activeMu.Lock()
			active[frame.RequestID] = cancel
			activeMu.Unlock()
			go func() {
				defer func() {
					activeMu.Lock()
					delete(active, frame.RequestID)
					activeMu.Unlock()
					cancel()
				}()
				handleRequest(reqCtx, ctx, conn, &writeMu, handler, frame)
			}()
		case FrameRequestCancel:
			activeMu.Lock()
			cancel := active[frame.RequestID]
			delete(active, frame.RequestID)
			activeMu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
	}
}

func handleRequest(ctx, writeCtx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, handler http.Handler, frame Frame) {
	reqURL := &url.URL{Scheme: "http", Host: "tethon-hostd", Path: frame.Path, RawQuery: frame.Query}
	req, err := http.NewRequestWithContext(ctx, frame.Method, reqURL.String(), bytes.NewReader(frame.Body))
	if err != nil {
		_ = writeFrame(writeCtx, conn, writeMu, Frame{
			Type:      FrameResponseError,
			RequestID: frame.RequestID,
			Status:    http.StatusBadRequest,
			Error:     err.Error(),
		})
		return
	}
	rw := &responseWriter{
		requestCtx: ctx,
		writeCtx:   writeCtx,
		conn:       conn,
		writeMu:    writeMu,
		requestID:  frame.RequestID,
		header:     make(http.Header),
		status:     http.StatusOK,
	}
	handler.ServeHTTP(rw, req)
	if ctx.Err() != nil {
		return
	}
	if err := rw.ensureStarted(); err != nil {
		return
	}
	_ = writeFrame(writeCtx, conn, writeMu, Frame{Type: FrameResponseEnd, RequestID: frame.RequestID})
}

type responseWriter struct {
	requestCtx context.Context
	writeCtx   context.Context
	conn       *websocket.Conn
	writeMu    *sync.Mutex
	requestID  string
	header     http.Header
	status     int
	started    bool
}

func (w *responseWriter) Header() http.Header {
	return w.header
}

func (w *responseWriter) WriteHeader(status int) {
	if w.started {
		return
	}
	w.status = status
	_ = w.ensureStarted()
}

func (w *responseWriter) Write(body []byte) (int, error) {
	if err := w.requestCtx.Err(); err != nil {
		return 0, err
	}
	if err := w.ensureStarted(); err != nil {
		return 0, err
	}
	if len(body) == 0 {
		return 0, nil
	}
	if err := writeFrame(w.writeCtx, w.conn, w.writeMu, Frame{
		Type:      FrameResponseBody,
		RequestID: w.requestID,
		Body:      body,
	}); err != nil {
		return 0, err
	}
	return len(body), nil
}

func (w *responseWriter) Flush() {
	_ = w.ensureStarted()
}

func (w *responseWriter) ensureStarted() error {
	if err := w.requestCtx.Err(); err != nil {
		return err
	}
	if w.started {
		return nil
	}
	w.started = true
	return writeFrame(w.writeCtx, w.conn, w.writeMu, Frame{
		Type:      FrameResponseStart,
		RequestID: w.requestID,
		Status:    w.status,
		Headers:   w.header.Clone(),
	})
}

func writeFrame(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, frame Frame) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	return wsjson.Write(ctx, conn, frame)
}
