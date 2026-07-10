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
