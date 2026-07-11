package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/genai"

	"botson/internal/networking/adkwire"
)

const testToken = "test-token"

func TestClient_Reachable(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		wantErrSub string
	}{
		{
			name: "ok",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
					t.Errorf("Authorization header = %q, want Bearer %s", got, testToken)
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`["Agent Botson"]`))
			},
		},
		{
			name: "unauthorized",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			wantErr:    true,
			wantErrSub: "rejected our token",
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr:    true,
			wantErrSub: "status 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			c := newClient(srv.URL, testToken)
			err := c.Reachable(context.Background())

			if tt.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tt.wantErrSub != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErrSub)) {
				t.Errorf("error = %v, want it to contain %q", err, tt.wantErrSub)
			}
		})
	}
}

func TestClient_CreateSession(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newClient(srv.URL, testToken)
	if err := c.CreateSession(context.Background(), "Agent Botson", "chat-alice", "sess-1"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// r.URL.Path is the *decoded* path -- the space proves url.PathEscape
	// correctly encoded "Agent Botson" on the wire and the server decoded
	// it back, rather than the request breaking on the literal space.
	const want = "/api/apps/Agent Botson/users/chat-alice/sessions/sess-1"
	if gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestClient_RunTurn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req adkwire.RunAgentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.AppName != "Agent Botson" || req.UserID != "chat-alice" || req.SessionID != "sess-1" {
			t.Errorf("unexpected request: %+v", req)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"author":"chat-alice","content":{"role":"user","parts":[{"text":"hi"}]}},
			{"author":"Agent Botson","content":{"role":"model","parts":[{"text":"hello!"}]}}
		]`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, testToken)
	events, err := c.RunTurn(context.Background(), adkwire.RunAgentRequest{
		AppName:   "Agent Botson",
		UserID:    "chat-alice",
		SessionID: "sess-1",
		NewMessage: genai.Content{
			Role:  "user",
			Parts: []*genai.Part{{Text: "hi"}},
		},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[1].Author != "Agent Botson" || events[1].Content.Parts[0].Text != "hello!" {
		t.Errorf("events[1] = %+v, want Agent Botson: hello!", events[1])
	}
}

func TestClient_ListSessions(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"s1","agentName":"Agent Botson","userId":"chat-alice","displayName":"hi","lastUpdateTime":1,"eventCount":2}]`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, testToken)
	sessions, err := c.ListSessions(context.Background(), "Agent Botson", "chat-alice")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "s1" {
		t.Errorf("sessions = %+v", sessions)
	}
	if gotQuery != "agent=Agent+Botson&user=chat-alice" {
		t.Errorf("query = %q, want agent/user filters", gotQuery)
	}
}

func TestClient_GetSession(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"s1","agentName":"Agent Botson","userId":"chat-alice","state":{"botson:autoMode":true},"events":[{"author":"chat-alice","text":"hi"}]}`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, testToken)
	detail, err := c.GetSession(context.Background(), "Agent Botson", "chat-alice", "sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	const want = "/botson/sessions/Agent Botson/chat-alice/sess-1"
	if gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if len(detail.Events) != 1 || detail.Events[0].Text != "hi" {
		t.Errorf("events = %+v", detail.Events)
	}
	if on, _ := detail.State["botson:autoMode"].(bool); !on {
		t.Errorf("state = %+v, want botson:autoMode=true", detail.State)
	}
}

func TestClient_DeleteSession(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newClient(srv.URL, testToken)
	if err := c.DeleteSession(context.Background(), "Agent Botson", "chat-alice", "sess-1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
	if gotPath != "/botson/sessions/Agent Botson/chat-alice/sess-1" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestClient_SetSessionAutoMode(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newClient(srv.URL, testToken)
	if err := c.SetSessionAutoMode(context.Background(), "Agent Botson", "chat-alice", "sess-1", true); err != nil {
		t.Fatalf("SetSessionAutoMode: %v", err)
	}
	if enabled, _ := gotBody["enabled"].(bool); !enabled {
		t.Errorf("body = %+v, want enabled=true", gotBody)
	}
}

func TestClient_ListAgents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"Agent Botson","description":"root","is_root":true,"tools":["listFiles"],"instructions":"be helpful","read_only":true}]`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, testToken)
	agents, err := c.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 1 || !agents[0].IsRoot || !agents[0].ReadOnly {
		t.Errorf("agents = %+v", agents)
	}
}

func TestClient_DeleteAgent(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newClient(srv.URL, testToken)
	if err := c.DeleteAgent(context.Background(), "Custom Agent"); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}
	if gotPath != "/botson/agents/Custom Agent" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestClient_GetSettings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model_name":"gemini-3.1-flash-lite","root_agent":"Agent Botson","host":"127.0.0.1","port":4222}`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, testToken)
	s, err := c.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if s.ModelName != "gemini-3.1-flash-lite" || s.Port != 4222 {
		t.Errorf("settings = %+v", s)
	}
}

func TestClient_UpdateSettings_OnlySendsNonNilFields(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model_name":"new-model","note":"restart required"}`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, testToken)
	modelName := "new-model"
	s, err := c.UpdateSettings(context.Background(), settingsPatch{ModelName: &modelName})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if s.Note != "restart required" {
		t.Errorf("note = %q, want %q", s.Note, "restart required")
	}
	if _, ok := gotBody["providerKeys"]; ok {
		t.Errorf("body = %+v, expected providerKeys to be omitted since it was nil", gotBody)
	}
	if gotBody["modelName"] != "new-model" {
		t.Errorf("body = %+v, want modelName=new-model", gotBody)
	}
}

func TestClient_UpdateSettings_NestsProviderKeys(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, testToken)
	geminiKey := "new-gemini-key"
	_, err := c.UpdateSettings(context.Background(), settingsPatch{
		ProviderKeys: &providerKeysPatch{Gemini: &geminiKey},
	})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}

	pk, ok := gotBody["providerKeys"].(map[string]any)
	if !ok {
		t.Fatalf("body = %+v, expected a providerKeys object", gotBody)
	}
	if pk["gemini"] != "new-gemini-key" {
		t.Errorf("providerKeys.gemini = %v, want new-gemini-key", pk["gemini"])
	}
	if _, ok := pk["openrouter"]; ok {
		t.Errorf("providerKeys = %+v, expected openrouter to be omitted since it was nil", pk)
	}
}

func TestClient_GetDashboardStats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"totalAgents":2,"totalSessions":5,"totalEvents":40,"dbPath":"/tmp/sessions.db","agents":[{"name":"Agent Botson","isRoot":true,"sessionCount":3}],"recentSessions":[{"id":"s1","agentName":"Agent Botson"}]}`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, testToken)
	stats, err := c.GetDashboardStats(context.Background())
	if err != nil {
		t.Fatalf("GetDashboardStats: %v", err)
	}
	if stats.TotalAgents != 2 || stats.TotalSessions != 5 || len(stats.Agents) != 1 {
		t.Errorf("stats = %+v", stats)
	}
}
