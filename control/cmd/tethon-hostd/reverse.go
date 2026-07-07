package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

	"tethon/internal/reverse"
)

type hubReverseConfig struct {
	minBackoff time.Duration
	maxBackoff time.Duration
	jitter     func(time.Duration) time.Duration
	hubURL     string
	logger     *slog.Logger
	status     *hostStatus
}

func (c hubReverseConfig) withDefaults() hubReverseConfig {
	if c.minBackoff <= 0 {
		c.minBackoff = hubRetryMinBackoff
	}
	if c.maxBackoff <= 0 {
		c.maxBackoff = hubRetryMaxBackoff
	}
	if c.maxBackoff < c.minBackoff {
		c.maxBackoff = c.minBackoff
	}
	if c.jitter == nil {
		c.jitter = defaultBackoffJitter
	}
	if c.logger == nil {
		c.logger = slog.Default()
	}
	return c
}

func runHubReverseLoop(ctx context.Context, hub reverseHub, hostID string, handler http.Handler, cfg hubReverseConfig) {
	cfg = cfg.withDefaults()
	retryDelay := cfg.minBackoff
	failures := 0

	for {
		connected := false
		err := hub.connectReverse(ctx, hostID, handler, func() {
			connected = true
			failures = 0
			retryDelay = cfg.minBackoff
			if cfg.status != nil {
				cfg.status.recordReverseConnected()
			}
			cfg.logger.Info("hub_reverse_connected", "hub_url", cfg.hubURL, "host_id", hostID)
		})
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("reverse connection closed")
		}
		failures++
		if cfg.status != nil {
			cfg.status.recordReverseFailure(err)
		}
		if connected {
			cfg.logger.Warn("hub_reverse_disconnected", "hub_url", cfg.hubURL, "host_id", hostID, "error", err.Error())
		} else {
			cfg.logger.Warn("hub_reverse_connect_failed", "hub_url", cfg.hubURL, "host_id", hostID, "error", err.Error(), "failed_attempts", failures)
		}
		delay := retryDelay + cfg.jitter(retryDelay)
		if !sleepContext(ctx, delay) {
			return
		}
		retryDelay = nextBackoff(retryDelay, cfg.maxBackoff)
	}
}

type reverseHub interface {
	connectReverse(context.Context, string, http.Handler, func()) error
}

func (c *hubClient) connectReverse(ctx context.Context, hostID string, handler http.Handler, onConnected func()) error {
	target, err := reverseURL(c.baseURL, hostID)
	if err != nil {
		return err
	}
	headers := http.Header{}
	if c.authToken != "" {
		headers.Set("Authorization", "Bearer "+c.authToken)
	}
	conn, _, err := websocket.Dial(ctx, target, &websocket.DialOptions{HTTPHeader: headers, HTTPClient: c.client})
	if err != nil {
		return err
	}
	conn.SetReadLimit(reverse.MaxFrameBytes)
	defer conn.Close(websocket.StatusNormalClosure, "hostd reverse connection closed")
	if onConnected != nil {
		onConnected()
	}
	return reverse.ServeRequests(ctx, conn, handler)
}

func reverseURL(baseURL, hostID string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported hub URL scheme %q", parsed.Scheme)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/hosts/" + hostID + "/connect"
	parsed.RawQuery = ""
	return parsed.String(), nil
}
