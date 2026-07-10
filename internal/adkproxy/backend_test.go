package adkproxy

import (
	"context"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"
)

// newEchoAgent returns a deterministic, no-LLM agent.Agent that echoes back
// the caller's latest "user" message, so this test doesn't depend on a real
// model backend or API key.
func newEchoAgent(t *testing.T) agent.Agent {
	t.Helper()

	a, err := agent.New(agent.Config{
		Name:        "echo_agent",
		Description: "test echo agent",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				text := "(no input)"
				if sess := ctx.Session(); sess != nil {
					for e := range sess.Events().All() {
						if e.Author == "user" && e.Content != nil && len(e.Content.Parts) > 0 && e.Content.Parts[0].Text != "" {
							text = e.Content.Parts[0].Text
						}
					}
				}
				ev := session.NewEvent(ctx, ctx.InvocationID())
				ev.Author = ctx.Agent().Name()
				ev.Content = genai.NewContentFromText("echo: "+text, genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("create echo agent: %v", err)
	}
	return a
}

// runAgentRequest mirrors adk-go's internal models.RunAgentRequest JSON
// shape (that type is unexported outside the ADK module, so we redeclare the
// wire-compatible subset we need).
type runAgentRequest struct {
	AppName    string        `json:"appName"`
	UserID     string        `json:"userId"`
	SessionID  string        `json:"sessionId"`
	NewMessage genai.Content `json:"newMessage"`
}

// TestStartBackend_EndToEnd drives the real ADK backend directly over plain
// HTTP -- no NATS, no reverse proxy in front -- confirming StartBackend
// still stands up a working REST server on its own.
func TestStartBackend_EndToEnd(t *testing.T) {
	echoAgent := newEchoAgent(t)

	runCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	b, err := StartBackend(runCtx, launcher.Config{
		AgentLoader:    agent.NewSingleLoader(echoAgent),
		SessionService: session.InMemoryService(),
	}, 0)
	if err != nil {
		t.Fatalf("StartBackend: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	ctx, cancelReq := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelReq()

	listReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, b.BaseURL()+"/api/list-apps", nil)
	listResp, err := client.Do(listReq)
	if err != nil {
		t.Fatalf("GET /api/list-apps: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/list-apps status = %d, want 200", listResp.StatusCode)
	}

	sessReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, b.BaseURL()+"/api/apps/echo_agent/users/testuser/sessions/testsession", nil)
	sessResp, err := client.Do(sessReq)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer sessResp.Body.Close()
	if sessResp.StatusCode != http.StatusOK {
		t.Fatalf("create session status = %d, want 200", sessResp.StatusCode)
	}

	reqBody, err := json.Marshal(runAgentRequest{
		AppName:    "echo_agent",
		UserID:     "testuser",
		SessionID:  "testsession",
		NewMessage: *genai.NewContentFromText("hello there", genai.RoleUser),
	})
	if err != nil {
		t.Fatalf("marshal run request: %v", err)
	}

	runReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, b.BaseURL()+"/api/run", strings.NewReader(string(reqBody)))
	runReq.Header.Set("Content-Type", "application/json")
	runResp, err := client.Do(runReq)
	if err != nil {
		t.Fatalf("run agent: %v", err)
	}
	defer runResp.Body.Close()
	if runResp.StatusCode != http.StatusOK {
		t.Fatalf("run agent status = %d, want 200", runResp.StatusCode)
	}

	respBody, err := io.ReadAll(runResp.Body)
	if err != nil {
		t.Fatalf("read run response: %v", err)
	}
	if !strings.Contains(string(respBody), "echo: hello there") {
		t.Errorf("run response = %s, want it to contain %q", respBody, "echo: hello there")
	}
}
