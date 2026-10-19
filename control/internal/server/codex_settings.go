package server

import (
	"context"
	"net/http"
	"path/filepath"
	"time"

	"tethon/internal/agent"
)

func (s *Server) handleCodexSettings(w http.ResponseWriter, r *http.Request) {
	adapter, ok := s.agents.Get("codex-cli")
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Codex is unavailable on this host")
		return
	}

	native, ok := adapter.(*agent.Codex)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "native Codex settings are unavailable")
		return
	}

	roots := append(append([]string{}, s.repoRoots...), s.repos.Paths()...)
	cwd := r.URL.Query().Get("cwd")
	if cwd == "" && len(roots) > 0 {
		cwd = roots[0]
	}
	if !filepath.IsAbs(cwd) {
		writeError(w, http.StatusBadRequest, "select an absolute working folder under an advertised repository")
		return
	}

	realCWD, err := resolveRealDir(cwd)
	if err != nil || !pathWithinAny(realCWD, roots) {
		writeError(w, http.StatusBadRequest, "the working folder is unavailable or outside the advertised repositories")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	settings, err := native.ReadSettings(ctx, realCWD)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, settings)
}
