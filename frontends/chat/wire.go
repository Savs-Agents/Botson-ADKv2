// The types below are local, hand-verified mirrors of the JSON wire shapes
// for /botson/*'s settings/agents/sessions/dashboard routes (docs/api.md
// §4) -- the same convention internal/networking/adkwire documents and
// justifies for /api/run: this package is a pure HTTP client with no
// import of internal/management (which would pull in the full ADK
// session/agent-loader machinery this frontend deliberately has no access
// to), so each server-side DTO gets a small mirror here instead, checked
// directly against internal/management/{sessions,dashboard,agents,
// config}.go, internal/engine/agent/loader.go, internal/config/config.go,
// and internal/networking/api/wire.go rather than assumed.
package chat

// sessionStat mirrors internal/management.SessionStat.
type sessionStat struct {
	ID             string `json:"id"`
	AgentName      string `json:"agentName"`
	UserID         string `json:"userId"`
	DisplayName    string `json:"displayName"`
	LastUpdateTime int64  `json:"lastUpdateTime"`
	EventCount     int    `json:"eventCount"`
}

// sessionEventSummary mirrors internal/management.SessionEventSummary.
type sessionEventSummary struct {
	Author    string `json:"author"`
	Timestamp string `json:"timestamp"`
	Text      string `json:"text"`
}

// sessionDetail mirrors internal/management.SessionDetail (SessionStat
// embedded/flattened server-side via json.Marshal's anonymous-field
// promotion, so its fields are pulled up to the top level here too).
type sessionDetail struct {
	sessionStat
	State  map[string]any        `json:"state"`
	Events []sessionEventSummary `json:"events"`
}

// agentStat mirrors internal/management.AgentStat.
type agentStat struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	IsRoot       bool   `json:"isRoot"`
	SessionCount int    `json:"sessionCount"`
}

// agentDetail mirrors internal/engine/agent.AgentDetail (AgentConfig
// embedded/flattened the same way).
type agentDetail struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	IsRoot       bool     `json:"is_root,omitempty"`
	Private      bool     `json:"private"`
	Tools        []string `json:"tools"`
	Instructions string   `json:"instructions"`
	ReadOnly     bool     `json:"read_only"`
}

// dashboardStats mirrors internal/management.DashboardStats.
type dashboardStats struct {
	TotalAgents    int           `json:"totalAgents"`
	TotalSessions  int           `json:"totalSessions"`
	TotalEvents    int           `json:"totalEvents"`
	DbPath         string        `json:"dbPath"`
	Agents         []agentStat   `json:"agents"`
	RecentSessions []sessionStat `json:"recentSessions"`
}

// providerKeys mirrors internal/config.ProviderKeys.
type providerKeys struct {
	Gemini     string `json:"gemini"`
	OpenRouter string `json:"openrouter"`
}

// settingsPayload mirrors internal/config.AppConfig as returned (masked)
// by GET /botson/settings and PATCH /botson/settings's reply
// (internal/networking/api.SettingsSetReply embeds config.AppConfig plus
// Note). api_auth_token is never included by the server -- see
// config.Mask -- so there's no field for it here.
type settingsPayload struct {
	ModelName     string       `json:"model_name"`
	ProviderKeys  providerKeys `json:"providerKeys"`
	RootAgent     string       `json:"root_agent"`
	Provider      string       `json:"provider"`
	WorkspaceRoot string       `json:"workspace_root"`
	Host          string       `json:"host"`
	Port          int          `json:"port"`
	Note          string       `json:"note,omitempty"`
}

// providerKeysPatch mirrors internal/networking/api.ProviderKeysPatch.
type providerKeysPatch struct {
	Gemini     *string `json:"gemini,omitempty"`
	OpenRouter *string `json:"openrouter,omitempty"`
}

// settingsPatch mirrors internal/networking/api.SettingsSetRequest -- only
// non-nil fields are sent, so a nil pointer here means "leave unchanged"
// on the server, not "clear the field." ProviderKeys is itself optional so
// a patch that doesn't touch either key can omit the object entirely.
type settingsPatch struct {
	ModelName     *string            `json:"modelName,omitempty"`
	RootAgent     *string            `json:"rootAgent,omitempty"`
	ProviderKeys  *providerKeysPatch `json:"providerKeys,omitempty"`
	WorkspaceRoot *string            `json:"workspaceRoot,omitempty"`
	Provider      *string            `json:"provider,omitempty"`
}

// sessionsSetAutoModeRequest mirrors
// internal/networking/api.SessionsSetAutoModeRequest.
type sessionsSetAutoModeRequest struct {
	Enabled bool `json:"enabled"`
}
