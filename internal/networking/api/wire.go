// The types below are the request/response wire shapes for the /botson/*
// routes (routes.go). Those routes cover settings, custom-agent CRUD, and
// session/dashboard management -- the things every CLI subcommand used to
// do by touching config.json/the session DB/~/.botson/agents directly.
package api

import "botson/internal/config"

// SettingsSetRequest changes only the fields that are non-nil, mirroring
// the old CLI's "only touch the flags you actually pass" semantics
// (cmd.Flags().Changed) now expressed on the wire as optional pointers.
type SettingsSetRequest struct {
	ModelName        *string `json:"modelName,omitempty"`
	RootAgent        *string `json:"rootAgent,omitempty"`
	GeminiAPIKey     *string `json:"geminiApiKey,omitempty"`
	WorkspaceRoot    *string `json:"workspaceRoot,omitempty"`
	Provider         *string `json:"provider,omitempty"`
	OpenRouterAPIKey *string `json:"openRouterApiKey,omitempty"`
}

// SettingsSetReply is PATCH /botson/settings's reply: the same fields
// GET /botson/settings returns (embedded, so they marshal at the top
// level), plus an optional Note -- set when modelName/provider changed,
// since this already-running core process won't actually use the new
// model until it's restarted.
type SettingsSetReply struct {
	config.AppConfig
	Note string `json:"note,omitempty"`
}

// AgentsSaveRequest is the request payload for POST /botson/agents.
type AgentsSaveRequest struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Tools        []string `json:"tools"`
	Private      bool     `json:"private"`
	Instructions string   `json:"instructions"`
}

// SessionsSetAutoModeRequest is the request payload for
// PATCH /botson/sessions/{agent}/{user}/{sessionId}/autoMode -- see
// management.AutoModeStateKey. The composite key itself travels as path
// params, not body fields, unlike its old NATS-subject predecessor.
type SessionsSetAutoModeRequest struct {
	Enabled bool `json:"enabled"`
}
