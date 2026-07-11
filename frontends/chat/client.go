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

// doJSON is the shared request/response plumbing every /botson/* method
// below uses: JSON-encode body (nil for no body), check the status code,
// and JSON-decode the reply into out (nil to just discard it after
// checking status) -- one place for this instead of each new method
// re-deriving its own status-check/body-read logic the way
// CreateSession/RunTurn (predating this helper) each do individually.
func (c *client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var reqBody []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("chat: marshal %s %s request: %w", method, path, err)
		}
		reqBody = b
	}

	resp, err := c.do(ctx, method, path, reqBody)
	if err != nil {
		return fmt.Errorf("chat: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("chat: read %s %s response: %w", method, path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("chat: %s %s failed: status %d: %s", method, path, resp.StatusCode, respBody)
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("chat: unmarshal %s %s response: %w", method, path, err)
		}
	}
	return nil
}

// ListSessions returns dashboard-shaped session summaries, most-recently-
// updated first, optionally narrowed by agent and/or user (empty string
// means "no filter" for that field).
func (c *client) ListSessions(ctx context.Context, agentFilter, userFilter string) ([]sessionStat, error) {
	q := url.Values{}
	if agentFilter != "" {
		q.Set("agent", agentFilter)
	}
	if userFilter != "" {
		q.Set("user", userFilter)
	}
	path := "/botson/sessions"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var stats []sessionStat
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &stats); err != nil {
		return nil, err
	}
	return stats, nil
}

// GetSession returns one session's full detail (state + event history) by
// its composite key.
func (c *client) GetSession(ctx context.Context, agent, user, sessionID string) (*sessionDetail, error) {
	path := "/botson/sessions/" + url.PathEscape(agent) + "/" + url.PathEscape(user) + "/" + url.PathEscape(sessionID)
	var detail sessionDetail
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

// DeleteSession removes a session by its composite key.
func (c *client) DeleteSession(ctx context.Context, agent, user, sessionID string) error {
	path := "/botson/sessions/" + url.PathEscape(agent) + "/" + url.PathEscape(user) + "/" + url.PathEscape(sessionID)
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil)
}

// SetSessionAutoMode toggles a session's auto-mode flag -- see AGENTS.md's
// "HITL confirmation wire protocol" §5's "Auto mode".
func (c *client) SetSessionAutoMode(ctx context.Context, agent, user, sessionID string, enabled bool) error {
	path := "/botson/sessions/" + url.PathEscape(agent) + "/" + url.PathEscape(user) + "/" + url.PathEscape(sessionID) + "/autoMode"
	return c.doJSON(ctx, http.MethodPatch, path, sessionsSetAutoModeRequest{Enabled: enabled}, nil)
}

// ListAgents returns every loaded agent (bundled defaults plus custom user
// agents).
func (c *client) ListAgents(ctx context.Context) ([]agentDetail, error) {
	var details []agentDetail
	if err := c.doJSON(ctx, http.MethodGet, "/botson/agents", nil, &details); err != nil {
		return nil, err
	}
	return details, nil
}

// DeleteAgent removes a custom user agent. Bundled defaults can't be
// deleted (management.ErrAgentNotFound comes back as a 404).
func (c *client) DeleteAgent(ctx context.Context, name string) error {
	return c.doJSON(ctx, http.MethodDelete, "/botson/agents/"+url.PathEscape(name), nil, nil)
}

// GetSettings returns the core's current (secret-masked) settings.
func (c *client) GetSettings(ctx context.Context) (*settingsPayload, error) {
	var s settingsPayload
	if err := c.doJSON(ctx, http.MethodGet, "/botson/settings", nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// UpdateSettings applies patch (only its non-nil fields) and returns the
// resulting settings, including a Note if a restart-required field
// (model/provider) changed.
func (c *client) UpdateSettings(ctx context.Context, patch settingsPatch) (*settingsPayload, error) {
	var s settingsPayload
	if err := c.doJSON(ctx, http.MethodPatch, "/botson/settings", patch, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// GetDashboardStats returns the aggregated system snapshot (totals,
// per-agent counts, recent sessions).
func (c *client) GetDashboardStats(ctx context.Context) (*dashboardStats, error) {
	var stats dashboardStats
	if err := c.doJSON(ctx, http.MethodGet, "/botson/dashboard/stats", nil, &stats); err != nil {
		return nil, err
	}
	return &stats, nil
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
