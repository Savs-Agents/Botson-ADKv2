// Package chat is Botson's built-in terminal chat client: a pure REST API
// client of internal/networking/api, with no access to (and no import of)
// internal/engine, internal/storage, or any other package that builds a
// Gemini model/agent registry/session service. It talks to an already-
// running `botson core` exactly like any external consumer would -- see
// AGENTS.md's "Unified core architecture" for why that boundary matters:
// the bug that redesign fixed was never "a frontend in this repo," it was
// a frontend embedding its own copy of the agent runtime.
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"botson/internal/networking/adkwire"
)

// client is a small typed wrapper around Botson's HTTP API.
type client struct {
	httpClient *http.Client
	baseURL    string
	authToken  string
}

func newClient(baseURL, authToken string) *client {
	return &client{
		httpClient: &http.Client{Timeout: 8 * time.Minute}, // a real agentic turn can run for minutes, not seconds -- see internal/networking/api/adkbackend.go's serverWriteTimeout doc comment
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		authToken:  authToken,
	}
}

func (c *client) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.authToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

// Reachable confirms the core is up and reachable with this client's
// token, via GET /api/list-apps -- this doubles as the "is core running"
// check chat.Run performs before doing anything else.
func (c *client) Reachable(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/api/list-apps", nil)
	if err != nil {
		return fmt.Errorf("chat: core unreachable at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("chat: core rejected our token (401) -- check ~/.botson/config.json's api_auth_token")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chat: core returned status %d: %s", resp.StatusCode, body)
	}
	return nil
}

// CreateSession creates a new, empty session under the given composite key.
func (c *client) CreateSession(ctx context.Context, agent, user, sessionID string) error {
	path := "/api/apps/" + url.PathEscape(agent) + "/users/" + url.PathEscape(user) + "/sessions/" + url.PathEscape(sessionID)
	resp, err := c.do(ctx, http.MethodPost, path, []byte{})
	if err != nil {
		return fmt.Errorf("chat: create session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chat: create session failed: status %d: %s", resp.StatusCode, body)
	}
	return nil
}

// RunTurn sends one turn and returns the full batch of events it produced.
// This client doesn't use the streaming routes (run_sse/run_live) -- see
// docs/api.md's note on them -- it waits for the whole turn instead.
func (c *client) RunTurn(ctx context.Context, req adkwire.RunAgentRequest) ([]adkwire.Event, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("chat: marshal run request: %w", err)
	}

	resp, err := c.do(ctx, http.MethodPost, "/api/run", body)
	if err != nil {
		return nil, fmt.Errorf("chat: run turn: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("chat: read run response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chat: run turn failed: status %d: %s", resp.StatusCode, respBody)
	}

	var events []adkwire.Event
	if err := json.Unmarshal(respBody, &events); err != nil {
		return nil, fmt.Errorf("chat: unmarshal run response: %w", err)
	}
	return events, nil
}
