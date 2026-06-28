package hub

import (
	"bufio"
	"bytes"
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
}

type hostRecord struct {
	protocol.RegisterHostRequest
	LastSeen time.Time
}

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
	mux := http.NewServeMux()
	mux.HandleFunc("/hosts", s.handleHosts)
	mux.HandleFunc("/hosts/", s.handleHost)
	mux.HandleFunc("/sessions", s.handleSessions)
	mux.HandleFunc("/sessions/", s.handleSession)
	return mux
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
	writeJSON(w, http.StatusOK, map[string]any{"hosts": summaries})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

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
	writeJSON(w, status, map[string]string{"error": message})
}

func (r hostRecord) String() string {
	return fmt.Sprintf("%s %s", r.HostID, r.URL)
}
