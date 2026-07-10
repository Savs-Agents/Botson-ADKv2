package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"google.golang.org/adk/v2/cmd/launcher"

	"botson/internal/engine/agent"
	"botson/internal/management"
)

// Mount registers every Botson REST route under /botson on r, closing over
// cfg for the handlers that need the agent loader/session service.
func Mount(r *mux.Router, cfg *launcher.Config) {
	r.HandleFunc("/botson/settings", handleSettingsGet).Methods(http.MethodGet)
	r.HandleFunc("/botson/settings", handleSettingsSet).Methods(http.MethodPatch)

	r.HandleFunc("/botson/agents", handleAgentsList).Methods(http.MethodGet)
	r.HandleFunc("/botson/agents/tools", handleAgentsTools).Methods(http.MethodGet)
	r.HandleFunc("/botson/agents", handleAgentsSave).Methods(http.MethodPost)
	r.HandleFunc("/botson/agents/{name}", handleAgentsDelete).Methods(http.MethodDelete)

	r.HandleFunc("/botson/sessions", handleSessionsList(cfg)).Methods(http.MethodGet)
	r.HandleFunc("/botson/sessions/{agent}/{user}/{sessionId}", handleSessionsGet(cfg)).Methods(http.MethodGet)
	r.HandleFunc("/botson/sessions/{agent}/{user}/{sessionId}", handleSessionsDelete(cfg)).Methods(http.MethodDelete)
	r.HandleFunc("/botson/sessions/{agent}/{user}/{sessionId}/autoMode", handleSessionsSetAutoMode(cfg)).Methods(http.MethodPatch)

	r.HandleFunc("/botson/dashboard/stats", handleDashboardStats(cfg)).Methods(http.MethodGet)
	r.HandleFunc("/botson/dashboard/users", handleDashboardUsers(cfg)).Methods(http.MethodGet)
}

// respondJSON writes v as a JSON body with the given status code.
func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// respondError replies with a bare {"error": "..."} object at a status
// code chosen by statusFor, so callers can distinguish e.g. a bad request
// from a not-found from a genuine server error, unlike the old NATS
// subjects (which had no status-code channel and always "replied 200"
// with the error embedded in the JSON payload alone).
func respondError(w http.ResponseWriter, err error) {
	respondJSON(w, statusFor(err), map[string]string{"error": err.Error()})
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, management.ErrInvalidAgentName):
		return http.StatusBadRequest
	case errors.Is(err, management.ErrAgentNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

// decodeJSON unmarshals r's body into v, replying with a 400 and returning
// false on failure so the caller can just `if !decodeJSON(...) { return }`.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	return true
}

func handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	cfg, err := management.GetMaskedConfig()
	if err != nil {
		respondError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, cfg)
}

func handleSettingsSet(w http.ResponseWriter, r *http.Request) {
	var req SettingsSetRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	cfg, modelOrProviderChanged, err := management.UpdateSettings(management.SettingsPatch{
		ModelName:        req.ModelName,
		RootAgent:        req.RootAgent,
		GeminiAPIKey:     req.GeminiAPIKey,
		WorkspaceRoot:    req.WorkspaceRoot,
		Provider:         req.Provider,
		OpenRouterAPIKey: req.OpenRouterAPIKey,
	})
	if err != nil {
		respondError(w, err)
		return
	}

	reply := SettingsSetReply{AppConfig: cfg}
	if modelOrProviderChanged {
		reply.Note = "modelName/provider are saved, but this running core process is still using the model it booted with -- restart the core for this change to actually take effect."
	}
	respondJSON(w, http.StatusOK, reply)
}

func handleAgentsList(w http.ResponseWriter, r *http.Request) {
	details, err := management.ListAgents()
	if err != nil {
		respondError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, details)
}

func handleAgentsTools(w http.ResponseWriter, r *http.Request) {
	tools, err := management.ListTools()
	if err != nil {
		respondError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, tools)
}

func handleAgentsSave(w http.ResponseWriter, r *http.Request) {
	var req AgentsSaveRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	detail := agent.AgentDetail{
		AgentConfig: agent.AgentConfig{
			Name:        req.Name,
			Description: req.Description,
			Private:     req.Private,
			Tools:       req.Tools,
		},
		Instructions: req.Instructions,
	}
	if err := management.SaveAgent(detail); err != nil {
		respondError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{})
}

func handleAgentsDelete(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if err := management.DeleteAgent(name); err != nil {
		respondError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{})
}

// sessionsAgentNames returns every known agent name, for ListSessions to
// scan across when no ?agent= filter narrows the request.
func sessionsAgentNames(cfg *launcher.Config) []string {
	if cfg == nil || cfg.AgentLoader == nil {
		return nil
	}
	return cfg.AgentLoader.ListAgents()
}

func handleSessionsList(cfg *launcher.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		stats, err := management.ListSessions(r.Context(), cfg.SessionService, sessionsAgentNames(cfg), q.Get("agent"), q.Get("user"))
		if err != nil {
			respondError(w, err)
			return
		}
		respondJSON(w, http.StatusOK, stats)
	}
}

func handleSessionsGet(cfg *launcher.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		detail, err := management.GetSession(r.Context(), cfg.SessionService, vars["agent"], vars["user"], vars["sessionId"])
		if err != nil {
			respondError(w, err)
			return
		}
		respondJSON(w, http.StatusOK, detail)
	}
}

func handleSessionsDelete(cfg *launcher.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		if err := management.DeleteSession(r.Context(), cfg.SessionService, vars["agent"], vars["user"], vars["sessionId"]); err != nil {
			respondError(w, err)
			return
		}
		respondJSON(w, http.StatusOK, map[string]string{})
	}
}

func handleSessionsSetAutoMode(cfg *launcher.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		var req SessionsSetAutoModeRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := management.SetSessionAutoMode(r.Context(), cfg.SessionService, vars["agent"], vars["user"], vars["sessionId"], req.Enabled, ""); err != nil {
			respondError(w, err)
			return
		}
		respondJSON(w, http.StatusOK, map[string]string{})
	}
}

func handleDashboardStats(cfg *launcher.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats, err := management.GetDashboardStats(r.Context(), cfg)
		if err != nil {
			respondError(w, err)
			return
		}
		respondJSON(w, http.StatusOK, stats)
	}
}

func handleDashboardUsers(cfg *launcher.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := management.ListSessionUsers(r.Context(), cfg)
		if err != nil {
			respondError(w, err)
			return
		}
		respondJSON(w, http.StatusOK, users)
	}
}
