package hub

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"tethon/internal/protocol"
)

type Server struct {
	mu          sync.RWMutex
	hosts       map[string]hostRecord
	hostTimeout time.Duration
	now         func() time.Time
	client      *http.Client
	authTokens  [][sha256.Size]byte
}

type hostRecord struct {
	protocol.RegisterHostRequest
	LastSeen time.Time
}

const sessionListFanoutTimeout = 5 * time.Second

func New(hostTimeout time.Duration) *Server {
	if hostTimeout <= 0 {
		hostTimeout = 30 * time.Second
	}
	return &Server{
		hosts:       make(map[string]hostRecord),
		hostTimeout: hostTimeout,
		now:         time.Now,
		client:      &http.Client{},
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
		next.ServeHTTP(w, r)
	})
}

func (s *Server) SetAuthToken(token string) {
	s.SetAuthTokens(token)
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

func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListHosts(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleHost(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/hosts/"), "/")
	if len(parts) == 1 && parts[0] == "register" && r.Method == http.MethodPost {
		s.handleRegisterHost(w, r)
		return
	}
	if len(parts) == 2 && parts[1] == "heartbeat" && r.Method == http.MethodPost {
		s.handleHeartbeat(w, r, parts[0])
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleRegisterHost(w http.ResponseWriter, r *http.Request) {
	var req protocol.RegisterHostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := validateHost(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name == "" {
		req.Name = req.HostID
	}

	s.mu.Lock()
	s.hosts[req.HostID] = hostRecord{RegisterHostRequest: req, LastSeen: s.now().UTC()}
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, s.summary(req.HostID))
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request, hostID string) {
	s.mu.Lock()
	record, ok := s.hosts[hostID]
	if ok {
		record.LastSeen = s.now().UTC()
		s.hosts[hostID] = record
	}
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "host not found")
		return
	}
	writeJSON(w, http.StatusOK, s.summary(hostID))
}

func (s *Server) handleListHosts(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	hostIDs := make([]string, 0, len(s.hosts))
	for hostID := range s.hosts {
		hostIDs = append(hostIDs, hostID)
	}
	s.mu.RUnlock()
	sort.Strings(hostIDs)

	summaries := make([]protocol.HostSummary, 0, len(hostIDs))
	for _, hostID := range hostIDs {
		summaries = append(summaries, s.summary(hostID))
	}
	writeJSON(w, http.StatusOK, protocol.ListHostsResponse{Hosts: summaries})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListSessions(w, r)
	case http.MethodPost:
		s.handleCreateSession(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	hosts := s.onlineHosts()
	summaries := make([]protocol.SessionSummary, 0)
	for _, host := range hosts {
		var hostResp protocol.ListSessionsResponse
		ctx, cancel := context.WithTimeout(r.Context(), sessionListFanoutTimeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(host.URL, "/")+"/sessions", nil)
		if err == nil {
			var status int
			status, err = s.doJSON(req, &hostResp)
			if status >= 300 {
				err = fmt.Errorf("host %s returned %d", host.HostID, status)
			}
		}
		cancel()
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		for _, session := range hostResp.Sessions {
			session.HostID = host.HostID
			session.ID = host.HostID + "/" + session.ID
			summaries = append(summaries, session)
		}
	}
	sort.Slice(summaries, func(i, j int) bool {
		if !summaries[i].CreatedAt.Equal(summaries[j].CreatedAt) {
			return summaries[i].CreatedAt.After(summaries[j].CreatedAt)
		}
		if summaries[i].HostID != summaries[j].HostID {
			return summaries[i].HostID < summaries[j].HostID
		}
		return summaries[i].ID < summaries[j].ID
	})
	writeJSON(w, http.StatusOK, protocol.ListSessionsResponse{Sessions: summaries})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req protocol.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HostID == "" {
		writeError(w, http.StatusBadRequest, "host_id is required")
		return
	}

	host, ok := s.onlineHost(req.HostID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "host is offline or unknown")
		return
	}

	hostReq := req
	hostReq.HostID = ""
	var hostResp protocol.CreateSessionResponse
	status, err := s.forwardJSON(r, host.URL+"/sessions", hostReq, &hostResp)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if status >= 300 {
		writeJSON(w, status, hostResp)
		return
	}
	hostResp.ID = req.HostID + "/" + hostResp.ID
	writeJSON(w, status, hostResp)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/sessions/")
	hostID, localID, suffix, ok := parseSessionPath(rest)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	host, ok := s.onlineHost(hostID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "host is offline or unknown")
		return
	}

	switch {
	case suffix == "stream" && r.Method == http.MethodGet:
		s.proxyStream(w, r, host.URL+"/sessions/"+localID+"/stream?"+r.URL.RawQuery)
	case suffix == "input" && r.Method == http.MethodPost:
		s.proxyJSON(w, r, host.URL+"/sessions/"+localID+"/input")
	case suffix == "cancel" && r.Method == http.MethodPost:
		s.proxyJSON(w, r, host.URL+"/sessions/"+localID+"/cancel")
	case strings.HasPrefix(suffix, "approvals/") && r.Method == http.MethodPost:
		requestID := strings.TrimPrefix(suffix, "approvals/")
		if requestID == "" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		s.proxyJSON(w, r, host.URL+"/sessions/"+localID+"/approvals/"+requestID)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) proxyJSON(w http.ResponseWriter, r *http.Request, target string) {
	var out any
	status, err := s.forwardBody(r, target, &out)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, status, out)
}

func (s *Server) proxyStream(w http.ResponseWriter, r *http.Request, target string) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	resp, err := s.client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if _, writeErr := w.Write(line); writeErr != nil {
				return
			}
			flusher.Flush()
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) forwardJSON(r *http.Request, target string, reqBody any, out any) (int, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	return s.doJSON(req, out)
}

func (s *Server) forwardBody(r *http.Request, target string, out any) (int, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	return s.doJSON(req, out)
}

func (s *Server) doJSON(req *http.Request, out any) (int, error) {
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

func (s *Server) onlineHost(hostID string) (hostRecord, bool) {
	s.mu.RLock()
	record, ok := s.hosts[hostID]
	s.mu.RUnlock()
	if !ok || !s.isOnline(record) {
		return hostRecord{}, false
	}
	return record, true
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
		HostID:    record.HostID,
		Name:      record.Name,
		URL:       record.URL,
		Agents:    record.Agents,
		RepoRoots: record.RepoRoots,
		Repos:     record.Repos,
		LastSeen:  record.LastSeen,
		Online:    s.isOnline(record),
	}
}

func (s *Server) isOnline(record hostRecord) bool {
	return s.now().Sub(record.LastSeen) <= s.hostTimeout
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
