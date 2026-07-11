package chat

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"botson/internal/config"
	"botson/internal/daemon"
)

// Run starts the interactive chat TUI against an already-running `botson
// core`. It requires the core to already be running -- chat never starts,
// stops, or otherwise manages background processes itself, keeping the
// "chat is a pure API client" boundary explicit (see the package doc
// comment). agentOverride, if non-empty, overrides cfg.RootAgent.
func Run(ctx context.Context, cfg *config.AppConfig, agentOverride string) error {
	agent := cfg.RootAgent
	if agentOverride != "" {
		agent = agentOverride
	}
	if agent == "" {
		return fmt.Errorf("chat: no agent configured (set root_agent in ~/.botson/config.json, or pass --agent)")
	}

	port, err := runningCorePort(cfg)
	if err != nil {
		return err
	}

	// Always loopback, regardless of the core's configured bind Host --
	// chat runs as a separate local process, not inside the core, so it
	// must dial a real connect-to address even if Host was reconfigured
	// to something like "0.0.0.0" (not itself dialable). Same rule
	// internal/automode's Run applies, for the same reason.
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	c := newClient(baseURL, cfg.ApiAuthToken)

	if err := c.Reachable(ctx); err != nil {
		return fmt.Errorf("%w\n\nIs the core actually up? Check with: botson core status", err)
	}

	sessionID := uuid.NewString()
	// "-" not ":" -- appName/userID/sessionID all get embedded literally as
	// filesystem directory names by internal/storage/artifact's
	// LocalFileService (getDir/getSessionDir), and ':' is a reserved
	// character in Windows paths (drive-letter/ADS syntax), which broke a
	// real Windows run with "The filename, directory name, or volume label
	// syntax is incorrect" the first time this shipped with "chat:" here.
	userID := "chat-" + localUsername()

	if err := c.CreateSession(ctx, agent, userID, sessionID); err != nil {
		return err
	}

	m := newModel(ctx, c, agent, userID, sessionID)
	// WithAltScreen: the dashboard is a fixed-size, multi-tab layout, not a
	// scrolling transcript -- without it, a tab whose rendered content is
	// even one line taller than the terminal (easy to hit across five
	// differently-shaped tabs) causes the terminal's own native scrolling,
	// which fights Bubble Tea's in-place repaint and can scroll the tab bar
	// itself out of view.
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// runningCorePort finds the port a currently-running `botson core` is
// actually bound to, via the same daemon state file `botson core
// status`/`stop` read (~/.botson/core.pid's Meta.apiPort) -- not
// cfg.Port directly, since a `botson core --port N` invocation can
// override the persisted config default for that one run without ever
// writing it back to disk (see cmd/botson-core/cmd_core.go's
// resolveHostPort), so config.json alone isn't reliable enough to find an
// already-running core.
func runningCorePort(cfg *config.AppConfig) (int, error) {
	status, err := daemon.GetStatus("core", "Botson core")
	if err != nil {
		return 0, fmt.Errorf("chat: failed to check core status: %w", err)
	}
	if !status.Running {
		return 0, fmt.Errorf("chat: botson core isn't running -- start it with: botson core start")
	}
	if raw, ok := status.Meta["apiPort"]; ok {
		if port, err := strconv.Atoi(raw); err == nil {
			return port, nil
		}
	}
	// Defensive fallback for an old daemon state file predating apiPort;
	// shouldn't happen against a current binary, since runCore always
	// writes it.
	return cfg.Port, nil
}

// localUsername mirrors internal/engine/agent/loader.go's {{USER}}
// placeholder resolution (os/user.Current, DOMAIN\ stripped on Windows,
// $USER/$USERNAME fallback) -- duplicated rather than imported, since
// pulling in internal/engine/agent here just for this small helper would
// give chat a dependency on the whole agent-loading package it has no
// other reason to need.
func localUsername() string {
	if u, err := user.Current(); err == nil {
		name := u.Username
		if idx := strings.LastIndex(name, "\\"); idx != -1 {
			name = name[idx+1:]
		}
		return name
	}
	if envUser := os.Getenv("USER"); envUser != "" {
		return envUser
	}
	if envUserWin := os.Getenv("USERNAME"); envUserWin != "" {
		return envUserWin
	}
	return "user"
}
