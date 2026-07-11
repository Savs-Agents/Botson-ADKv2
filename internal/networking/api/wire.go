// The types below are the request/response wire shapes for the /botson/*
// routes (routes.go). Those routes cover settings, custom-agent CRUD, and
// session/dashboard management -- the things every CLI subcommand used to
// do by touching config.json/the session DB/~/.botson/agents directly.
package api

import "botson/internal/config"

// ProviderKeysPatch mirrors config.ProviderKeys, but every field is an
// optional pointer (like SettingsSetRequest's own fields) so a client can
// change just one provider's key without having to resend the other.
type ProviderKeysPatch struct {
	Gemini     *string `json:"gemini,omitempty"`
	OpenRouter *string `json:"openrouter,omitempty"`
}

// SettingsSetRequest changes only the fields that are non-nil, mirroring
// the old CLI's "only touch the flags you actually pass" semantics
// (cmd.Flags().Changed) now expressed on the wire as optional pointers.
// ProviderKeys is itself optional so a request that doesn't touch either
// key can omit the object entirely rather than send two nulls.
type SettingsSetRequest struct {
	ModelName     *string            `json:"modelName,omitempty"`
	RootAgent     *string            `json:"rootAgent,omitempty"`
	ProviderKeys  *ProviderKeysPatch `json:"providerKeys,omitempty"`
	WorkspaceRoot *string            `json:"workspaceRoot,omitempty"`
	Provider      *string            `json:"provider,omitempty"`
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
