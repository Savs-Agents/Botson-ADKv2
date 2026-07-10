package apiserver

import (
	"context"
	"encoding/json"
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

type runAgentRequest struct {
	AppName    string        `json:"appName"`
	UserID     string        `json:"userId"`
	SessionID  string        `json:"sessionId"`
	NewMessage genai.Content `json:"newMessage"`
}

// TestServer_EndToEnd is the successor to the old adkgateway package's
// TestGateway_EndToEnd, minus NATS entirely: it drives the whole composed
// stack (auth middleware + ADK reverse proxy + botsonapi) over plain HTTP,
// confirming both the ADK surface and Botson's own REST surface are
// reachable through one authenticated server, and that an unauthenticated
// request to either is rejected.
func TestServer_EndToEnd(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const token = "test-token"
	echoAgent := newEchoAgent(t)

	srv, err := New(Config{
		Host:      "127.0.0.1",
		Port:      0,
		AuthToken: token,
		ADK: launcher.Config{
			AgentLoader:    agent.NewSingleLoader(echoAgent),
			SessionService: session.InMemoryService(),
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- srv.Run(runCtx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-runErr:
			if err != nil {
				t.Errorf("Server.Run returned error on shutdown: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("Server.Run did not shut down in time")
		}
	})

	// Run() binds the listener synchronously before doing anything slow
	// (starting the ADK backend can take longer), so poll Addr() first.
	var addr string
	deadline := time.Now().Add(15 * time.Second)
	for {
		if addr = srv.Addr(); addr != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server never bound an address")
		}
		time.Sleep(10 * time.Millisecond)
	}
	baseURL := "http://" + addr

	client := &http.Client{Timeout: 5 * time.Second}
	ctx, cancelReq := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelReq()

	doAuthed := func(method, path string, body []byte) *http.Response {
		t.Helper()
		var deadline = time.Now().Add(15 * time.Second)
		for {
			var reqBody *strings.Reader
			if body != nil {
				reqBody = strings.NewReader(string(body))
			} else {
				reqBody = strings.NewReader("")
			}
			req, _ := http.NewRequestWithContext(ctx, method, baseURL+path, reqBody)
			req.Header.Set("Authorization", "Bearer "+token)
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := client.Do(req)
			if err == nil {
				return resp
			}
			if time.Now().After(deadline) {
				t.Fatalf("request to %s never succeeded: %v", path, err)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Unauthenticated requests to both surfaces are rejected.
	noAuthReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/list-apps", nil)
	noAuthResp, err := client.Do(noAuthReq)
	if err != nil {
		t.Fatalf("unauthenticated request: %v", err)
	}
	noAuthResp.Body.Close()
	if noAuthResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated GET /api/list-apps status = %d, want 401", noAuthResp.StatusCode)
	}

	noAuthResp2, err := client.Do(mustRequest(ctx, http.MethodGet, baseURL+"/botson/dashboard/stats"))
	if err != nil {
		t.Fatalf("unauthenticated request: %v", err)
	}
	noAuthResp2.Body.Close()
	if noAuthResp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated GET /botson/dashboard/stats status = %d, want 401", noAuthResp2.StatusCode)
	}

	// Authenticated requests to the ADK surface work end to end.
	listResp := doAuthed(http.MethodGet, "/api/list-apps", nil)
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/list-apps status = %d, want 200", listResp.StatusCode)
	}

	sessResp := doAuthed(http.MethodPost, "/api/apps/echo_agent/users/testuser/sessions/testsession", []byte{})
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
	runResp := doAuthed(http.MethodPost, "/api/run", reqBody)
	defer runResp.Body.Close()
	if runResp.StatusCode != http.StatusOK {
		t.Fatalf("run agent status = %d, want 200", runResp.StatusCode)
	}

	// Authenticated requests to Botson's own REST surface work too, on the
	// same server/port.
	statsResp := doAuthed(http.MethodGet, "/botson/dashboard/stats", nil)
	defer statsResp.Body.Close()
	if statsResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /botson/dashboard/stats status = %d, want 200", statsResp.StatusCode)
	}
}

func mustRequest(ctx context.Context, method, url string) *http.Request {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		panic(err)
	}
	return req
}
