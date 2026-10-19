package hub

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"tethon/internal/protocol"
	"tethon/internal/reverse"
)

type Server struct {
	mu                sync.RWMutex
	hosts             map[string]hostRecord
	reverse           map[string]*reverseConnection
	approvalPushKeys  map[string]struct{}
	approvalPushOrder []string
	hostTimeout       time.Duration
	now               func() time.Time
	authTokens        [][sha256.Size]byte
	devices           *DeviceStore
	push              PushSender
	logger            *slog.Logger
}

type hostRecord struct {
	protocol.RegisterHostRequest
	LastSeen time.Time
}

const sessionListFanoutTimeout = 5 * time.Second
const approvalPushDedupeLimit = 256

func New(hostTimeout time.Duration) *Server {
	if hostTimeout <= 0 {
		hostTimeout = 30 * time.Second
	}
	return &Server{
		hosts:            make(map[string]hostRecord),
		reverse:          make(map[string]*reverseConnection),
		approvalPushKeys: make(map[string]struct{}),
		hostTimeout:      hostTimeout,
		now:              time.Now,
		logger:           slog.Default(),
	}
}

func (s *Server) Handler() http.Handler {
	next := s.humaHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if hostID, ok := reverseConnectHostID(r); ok {
			s.handleReverseConnect(w, r, hostID)
			return
		}
		if hostID, ok := approvalNotifyHostID(r); ok {
			s.handleApprovalNotify(w, r, hostID)
			return
		}
		if hostID, ok := approvalResolvedNotifyHostID(r); ok {
			s.handleApprovalResolvedNotify(w, r, hostID)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func reverseConnectHostID(r *http.Request) (string, bool) {
	if r.Method != http.MethodGet {
		return "", false
	}
	rest := strings.TrimPrefix(r.URL.Path, "/hosts/")
	if rest == r.URL.Path {
		return "", false
	}
	hostID, suffix, ok := strings.Cut(rest, "/")
	return hostID, ok && hostID != "" && suffix == "connect"
}

func approvalNotifyHostID(r *http.Request) (string, bool) {
	if r.Method != http.MethodPost {
		return "", false
	}
	rest := strings.TrimPrefix(r.URL.Path, "/hosts/")
	if rest == r.URL.Path {
		return "", false
	}
	hostID, suffix, ok := strings.Cut(rest, "/")
	return hostID, ok && hostID != "" && suffix == "notify/approval"
}

func (s *Server) handleReverseConnect(w http.ResponseWriter, r *http.Request, hostID string) {
	if _, ok := s.hostRecord(hostID); !ok {
		writeError(w, http.StatusNotFound, "host not found")
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.logger.Warn("hub_reverse_accept_failed", "host_id", hostID, "error", err.Error())
		return
	}
	conn.SetReadLimit(reverse.MaxFrameBytes)
	reverse := newReverseConnection(s, hostID, conn)
	s.setReverseConnection(hostID, reverse)
	s.logger.Info("hub_reverse_connected", "host_id", hostID)
	if err := reverse.run(r.Context()); err != nil {
		s.logger.Warn("hub_reverse_read_failed", "host_id", hostID, "error", err.Error())
	}
	s.removeReverseConnection(hostID, reverse)
	s.logger.Info("hub_reverse_disconnected", "host_id", hostID)
}

func (s *Server) SetAuthToken(token string) {
	s.SetAuthTokens(token)
}

func (s *Server) SetDeviceStore(store *DeviceStore) {
	s.devices = store
}

func (s *Server) SetPushSender(sender PushSender) {
	s.push = sender
}

func (s *Server) SetAuthTokens(tokens ...string) {
	s.authTokens = make([][sha256.Size]byte, 0, len(tokens))
	for _, token := range ParseAuthTokens(tokens...) {
		s.authTokens = append(s.authTokens, sha256.Sum256([]byte(token)))
	}
}

func ParseAuthTokens(values ...string) []string {
	tokens := make([]string, 0, len(values))
	for _, value := range values {
		for token := range strings.FieldsFuncSeq(value, func(r rune) bool {
			return r == ',' || r == '\n'
		}) {
			token = strings.TrimSpace(token)
			if token != "" {
				tokens = append(tokens, token)
			}
		}
	}
	return tokens
}

func (s *Server) authorized(r *http.Request) bool {
	if len(s.authTokens) == 0 {
		return true
	}
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	got := sha256.Sum256([]byte(token))
	matched := 0
	for _, want := range s.authTokens {
		matched |= subtle.ConstantTimeCompare(got[:], want[:])
	}
	return matched == 1
}

func (s *Server) onlineHost(hostID string) (hostRecord, bool) {
	record, ok := s.hostRecord(hostID)
	if !ok || !s.isOnline(record) {
		return hostRecord{}, false
	}
	return record, true
}

func (s *Server) hostRecord(hostID string) (hostRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.hosts[hostID]
	return record, ok
}

func (s *Server) onlineHosts() []hostRecord {
	s.mu.RLock()
	records := make([]hostRecord, 0, len(s.hosts))
	for _, record := range s.hosts {
		if s.isOnline(record) {
			records = append(records, record)
		}
	}
	s.mu.RUnlock()

	sort.Slice(records, func(i, j int) bool {
		return records[i].HostID < records[j].HostID
	})
	return records
}

func (s *Server) summary(hostID string) protocol.HostSummary {
	s.mu.RLock()
	record := s.hosts[hostID]
	s.mu.RUnlock()
	return protocol.HostSummary{
		HostID:      record.HostID,
		Name:        record.Name,
		URL:         record.URL,
		Agents:      record.Agents,
		AgentStatus: record.AgentStatus,
		RepoRoots:   record.RepoRoots,
		Repos:       record.Repos,
		LastSeen:    record.LastSeen,
		Online:      s.isOnline(record),
	}
}

func (s *Server) logHostRegistered(host protocol.HostSummary) {
	s.logger.Info(
		"hub_host_registered",
		"host_id", host.HostID,
		"name", host.Name,
		"url", host.URL,
		"repo_count", len(host.Repos),
		"agent_count", len(host.Agents),
		"online", host.Online,
	)
}

func (s *Server) logHostHeartbeat(host protocol.HostSummary) {
	s.logger.Info(
		"hub_host_heartbeat",
		"host_id", host.HostID,
		"url", host.URL,
		"last_seen", host.LastSeen,
		"online", host.Online,
	)
}

func (s *Server) isOnline(record hostRecord) bool {
	s.mu.RLock()
	reverse := s.reverse[record.HostID]
	s.mu.RUnlock()
	return reverse != nil && !reverse.closed()
}

func validateHost(req protocol.RegisterHostRequest) error {
	if req.HostID == "" {
		return errors.New("host_id is required")
	}
	if strings.Contains(req.HostID, "/") {
		return errors.New("host_id cannot contain slash")
	}
	if req.URL == "" {
		return errors.New("url is required")
	}
	parsed, err := url.Parse(req.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("url must be absolute")
	}
	return nil
}

func validatePushApprovalTest(req protocol.PushApprovalTestRequest) error {
	if strings.TrimSpace(req.HostID) == "" {
		return errors.New("host_id is required")
	}
	if strings.TrimSpace(req.SessionID) == "" {
		return errors.New("session_id is required")
	}
	if strings.TrimSpace(req.RequestID) == "" {
		return errors.New("request_id is required")
	}
	return nil
}

func parseSessionPath(path string) (hostID, localID, suffix string, ok bool) {
	parts := strings.SplitN(path, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, protocol.ErrorResponse{Error: message})
}

func (r hostRecord) String() string {
	return fmt.Sprintf("%s %s", r.HostID, r.URL)
}
