package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"

	"botson/internal/management"
)

func newTestRouter(t *testing.T, cfg *launcher.Config) *mux.Router {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	r := mux.NewRouter()
	Mount(r, cfg)
	return r
}

func doRequest(t *testing.T, r *mux.Router, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestSettingsGetAndSet(t *testing.T) {
	r := newTestRouter(t, &launcher.Config{})

	rec := doRequest(t, r, http.MethodGet, "/botson/settings", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /botson/settings status = %d, want 200; body=%s", rec.Code, rec.Body)
	}

	newRoot := t.TempDir()
	rec = doRequest(t, r, http.MethodPatch, "/botson/settings", SettingsSetRequest{WorkspaceRoot: &newRoot})
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /botson/settings status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	var reply SettingsSetReply
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	if reply.WorkspaceRoot != newRoot {
		t.Errorf("WorkspaceRoot = %q, want %q", reply.WorkspaceRoot, newRoot)
	}
	if reply.Note != "" {
		t.Errorf("expected no restart-needed Note for a WorkspaceRoot-only change, got %q", reply.Note)
	}

	newModel := "gemini-3.1-pro"
	rec = doRequest(t, r, http.MethodPatch, "/botson/settings", SettingsSetRequest{ModelName: &newModel})
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	if reply.Note == "" {
		t.Errorf("expected a restart-needed Note for a ModelName change, got none")
	}
}

func TestAgentsListToolsSaveDelete(t *testing.T) {
	r := newTestRouter(t, &launcher.Config{})

	rec := doRequest(t, r, http.MethodGet, "/botson/agents", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /botson/agents status = %d, want 200; body=%s", rec.Code, rec.Body)
	}

	rec = doRequest(t, r, http.MethodGet, "/botson/agents/tools", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /botson/agents/tools status = %d, want 200; body=%s", rec.Code, rec.Body)
	}

	rec = doRequest(t, r, http.MethodPost, "/botson/agents", AgentsSaveRequest{
		Name:        "Test Agent",
		Description: "a test agent",
		Tools:       []string{"readFile"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /botson/agents status = %d, want 200; body=%s", rec.Code, rec.Body)
	}

	rec = doRequest(t, r, http.MethodDelete, "/botson/agents/Test%20Agent", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /botson/agents/Test%%20Agent status = %d, want 200; body=%s", rec.Code, rec.Body)
	}

	rec = doRequest(t, r, http.MethodDelete, "/botson/agents/Nonexistent%20Agent", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE of a nonexistent agent status = %d, want 404; body=%s", rec.Code, rec.Body)
	}
}

func TestSessionsListGetDeleteSetAutoMode(t *testing.T) {
	svc := session.InMemoryService()
	loader := agent.NewSingleLoader(mustEchoAgent(t))
	cfg := &launcher.Config{SessionService: svc, AgentLoader: loader}
	r := newTestRouter(t, cfg)

	createResp, err := svc.Create(t.Context(), &session.CreateRequest{AppName: loader.RootAgent().Name(), UserID: "u1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess := createResp.Session

	rec := doRequest(t, r, http.MethodGet, "/botson/sessions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /botson/sessions status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	var stats []management.SessionStat
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("unmarshal stats: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 session, got %d", len(stats))
	}

	path := "/botson/sessions/" + sess.AppName() + "/" + sess.UserID() + "/" + sess.ID()

	rec = doRequest(t, r, http.MethodGet, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200; body=%s", path, rec.Code, rec.Body)
	}

	rec = doRequest(t, r, http.MethodPatch, path+"/autoMode", SessionsSetAutoModeRequest{Enabled: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH %s/autoMode status = %d, want 200; body=%s", path, rec.Code, rec.Body)
	}

	getResp, err := svc.Get(t.Context(), &session.GetRequest{AppName: sess.AppName(), UserID: sess.UserID(), SessionID: sess.ID()})
	if err != nil {
		t.Fatalf("get session directly: %v", err)
	}
	if on, _ := getResp.Session.State().Get(management.AutoModeStateKey); on != true {
		t.Errorf("expected AutoModeStateKey=true after PATCH .../autoMode, got %v", on)
	}

	rec = doRequest(t, r, http.MethodDelete, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE %s status = %d, want 200; body=%s", path, rec.Code, rec.Body)
	}
}

func TestDashboardStatsAndUsers(t *testing.T) {
	svc := session.InMemoryService()
	loader := agent.NewSingleLoader(mustEchoAgent(t))
	cfg := &launcher.Config{SessionService: svc, AgentLoader: loader}
	r := newTestRouter(t, cfg)

	rec := doRequest(t, r, http.MethodGet, "/botson/dashboard/stats", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /botson/dashboard/stats status = %d, want 200; body=%s", rec.Code, rec.Body)
	}

	rec = doRequest(t, r, http.MethodGet, "/botson/dashboard/users", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /botson/dashboard/users status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
}

// mustEchoAgent builds a minimal no-LLM agent.Agent, just enough to give
// AgentLoader-dependent handlers (sessions/dashboard) a root agent to work
// with.
func mustEchoAgent(t *testing.T) agent.Agent {
	t.Helper()
	a, err := agent.New(agent.Config{Name: "echo_agent", Description: "test agent"})
	if err != nil {
		t.Fatalf("create echo agent: %v", err)
	}
	return a
}
