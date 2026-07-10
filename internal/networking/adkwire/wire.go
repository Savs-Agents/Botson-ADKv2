// Package adkwire holds the JSON wire shapes for ADK's REST API
// (POST /api/run specifically) that more than one Go client in this module
// needs: internal/automode (auto-answering HITL confirmations) and
// frontends/chat (the interactive terminal chat client). ADK's own request/
// response DTOs (server/adkrest/internal/models) live under an internal/
// package this module can't import across module boundaries, so these are
// hand-verified mirrors -- checked directly against the vendored ADK
// source (server/adkrest/internal/models/runtime.go's RunAgentRequest, and
// server/adkrest/internal/models/event.go's Event/FromSessionEvent) rather
// than assumed. genai.Content/Part/FunctionCall/FunctionResponse (the
// public google.golang.org/genai module) already have correct camelCase
// JSON tags, so RunAgentRequest uses genai.Content directly instead of
// hand-rolling a parallel Content/Part mirror.
package adkwire

import "google.golang.org/genai"

// RunAgentRequest is POST /api/run's request body. Kept minimal: only the
// fields any client in this module actually sends (appName/userId/
// sessionId/newMessage) -- ADK's own RunAgentRequest additionally supports
// streaming/stateDelta/functionCallEventId, all optional and unused here.
type RunAgentRequest struct {
	AppName    string        `json:"appName"`
	UserID     string        `json:"userId"`
	SessionID  string        `json:"sessionId"`
	NewMessage genai.Content `json:"newMessage"`
}

// Event is the minimal subset of ADK's response DTO (models.Event, built
// via models.FromSessionEvent for every event POST /api/run returns) that
// this module's clients need to render a turn: who said it, and what they
// said/called. json.Unmarshal silently ignores the many other fields
// models.Event carries (actions, timestamps, branch, ...) that aren't
// needed here.
type Event struct {
	Author  string         `json:"author"`
	Content *genai.Content `json:"content"`
}
