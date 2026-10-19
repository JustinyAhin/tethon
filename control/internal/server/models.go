package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"tethon/internal/agent"
	"tethon/internal/protocol"
)

const modelCatalogTimeout = 10 * time.Second

func (s *Server) handleAgentModels(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/agents/"), "/")
	if len(parts) != 2 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if parts[0] == "codex-cli" {
		switch parts[1] {
		case "settings":
			s.handleCodexSettings(w, r)
			return
		case "conversations":
			s.handleCodexConversations(w, r)
			return
		}
	}

	if parts[1] != "models" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	adapter, ok := s.agents.Get(parts[0])
	if !ok {
		writeError(w, http.StatusNotFound, "unknown agent")
		return
	}

	catalog, err := loadModelCatalog(r.Context(), adapter)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, catalog)
}

func loadModelCatalog(ctx context.Context, adapter agent.Adapter) (protocol.ModelCatalog, error) {
	lister, ok := adapter.(agent.ModelLister)
	if !ok {
		return protocol.ModelCatalog{
			Agent:   adapter.Name(),
			Status:  protocol.ModelCatalogUnavailable,
			Message: "model selection is not available for this agent",
			Models:  []protocol.ModelInfo{},
		}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, modelCatalogTimeout)
	defer cancel()

	catalog, err := lister.ListModels(ctx)
	if err != nil {
		return protocol.ModelCatalog{}, fmt.Errorf("cannot load agent models: %w", err)
	}

	catalog.Agent = adapter.Name()
	if catalog.Status == "" {
		catalog.Status = protocol.ModelCatalogReady
	}
	if catalog.Status != protocol.ModelCatalogReady && catalog.Status != protocol.ModelCatalogUnavailable {
		return protocol.ModelCatalog{}, fmt.Errorf("agent returned an invalid model catalog status")
	}
	if catalog.Models == nil {
		catalog.Models = []protocol.ModelInfo{}
	}
	if catalog.Status == protocol.ModelCatalogUnavailable {
		catalog.Models = []protocol.ModelInfo{}
	}

	return catalog, nil
}

func validateRequestedModel(ctx context.Context, adapter agent.Adapter, requested string) (int, error) {
	if requested == "" {
		return 0, nil
	}

	catalog, err := loadModelCatalog(ctx, adapter)
	if err != nil {
		return http.StatusBadGateway, err
	}

	if catalog.Status == protocol.ModelCatalogReady {
		for _, model := range catalog.Models {
			if model.ID == requested {
				return 0, nil
			}
		}
	}

	return http.StatusBadRequest, fmt.Errorf("requested model is not available for this agent")
}
