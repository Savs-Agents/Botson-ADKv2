// Package automode is a background watcher that keeps a session moving even
// after every human client has disconnected: any session flagged with
// management.AutoModeStateKey in its own durable state gets its pending HITL
// confirmations (see AGENTS.md's "HITL confirmation wire protocol") answered
// automatically -- the same {"confirmed": true} answer a connected
// Botson-TUI already gives instantly for a human, just running unattended
// inside the core process as a fallback once nothing is connected to do it.
//
// This is deliberately built as one more HTTP client of Botson's own public
// API (internal/apiserver), calling POST /api/run over loopback with the
// same bearer token any external consumer would use, rather than reaching
// into the in-process Runner/session state directly -- there's no hook
// anywhere (ADK's own module, or Botson's) for "a turn just paused for
// confirmation," so polling the shared session service directly for
// pending confirmations, then re-issuing the run call exactly like a
// human-driven client would, is the same approach either way. Going
// through the real HTTP surface additionally means this code path is
// continuously exercised by the core's own background worker, not just by
// external consumers.
package automode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/toolconfirmation"

	"botson/internal/management"
)

// pollInterval bounds how long a pending confirmation can sit unanswered
// after every client disconnects before this worker catches it -- a
// connected Botson-TUI answers instantly on its own, so this is a latency
// ceiling for the unattended case only, not the common path.
const pollInterval = 5 * time.Second

// maxConsecutiveApprovals caps how many times in a row this worker will
// auto-approve one session without that session's auto-mode flag having
// been freshly (re-)enabled in between, so a runaway gated-tool loop can't
// burn API cost or do damage indefinitely while nobody's watching. Once
// hit, auto mode is turned back off for that session and the reason is
// recorded in its own history.
const maxConsecutiveApprovals = 25

// Run polls every session known to cfg's agent loader, auto-answering any
// confirmation left pending on a session flagged for auto mode, until ctx
// is done. baseURL is Botson's own HTTP API server's loopback address (e.g.
// "http://127.0.0.1:4222") -- always loopback regardless of what interface
// the server is actually bound to, since this worker runs in-process on the
// same machine. authToken is the same bearer token any external consumer
// authenticates with.
func Run(ctx context.Context, baseURL, authToken string, cfg *launcher.Config) error {
	w := &worker{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		authToken:  authToken,
		streak:     make(map[string]int),
		wasOn:      make(map[string]bool),
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

type worker struct {
	cfg        *launcher.Config
	httpClient *http.Client
	baseURL    string
	authToken  string

	mu     sync.Mutex
	streak map[string]int  // sessionKey -> auto-approvals given since auto mode was last (re-)enabled
	wasOn  map[string]bool // sessionKey -> last-observed AutoModeStateKey value, to detect a fresh enable
}

func sessionKey(appName, userID, sessionID string) string {
	return appName + "\x00" + userID + "\x00" + sessionID
}

// tick scans every session of every known agent for pending confirmations.
// Sequential, not one goroutine per session -- auto mode has no throughput
// requirement, and serial processing trivially rules out two ticks racing
// to answer the same confirmation twice.
func (w *worker) tick(ctx context.Context) {
	if w.cfg == nil || w.cfg.AgentLoader == nil || w.cfg.SessionService == nil {
		return
	}
	for _, appName := range w.cfg.AgentLoader.ListAgents() {
		resp, err := w.cfg.SessionService.List(ctx, &session.ListRequest{AppName: appName})
		if err != nil || resp == nil {
			continue
		}
		for _, sess := range resp.Sessions {
			w.checkSession(ctx, sess)
		}
	}
}

func (w *worker) checkSession(ctx context.Context, sess session.Session) {
	enabledVal, _ := sess.State().Get(management.AutoModeStateKey)
	on, _ := enabledVal.(bool)

	key := sessionKey(sess.AppName(), sess.UserID(), sess.ID())

	w.mu.Lock()
	justEnabled := on && !w.wasOn[key]
	w.wasOn[key] = on
	if justEnabled {
		w.streak[key] = 0
	}
	streak := w.streak[key]
	w.mu.Unlock()

	if !on {
		return
	}

	// session.Service.List (which found sess) returns lightweight sessions
	// carrying only state, not event history -- see the database-backed
	// implementation's own List, which never touches events. A full Get is
	// needed to actually see what's pending.
	full, err := w.cfg.SessionService.Get(ctx, &session.GetRequest{AppName: sess.AppName(), UserID: sess.UserID(), SessionID: sess.ID()})
	if err != nil || full == nil {
		return
	}
	sess = full.Session

	pending := pendingConfirmations(sess)
	if len(pending) == 0 {
		return
	}

	if streak+len(pending) > maxConsecutiveApprovals {
		reason := fmt.Sprintf("auto mode turned itself off after %d consecutive auto-approvals with no re-enable in between (safety cap)", streak)
		if err := management.SetSessionAutoMode(ctx, w.cfg.SessionService, sess.AppName(), sess.UserID(), sess.ID(), false, reason); err == nil {
			w.mu.Lock()
			w.wasOn[key] = false
			w.mu.Unlock()
		}
		return
	}

	if err := w.autoApprove(ctx, sess, pending); err != nil {
		return // transient failure -- the next tick retries from the same, still-pending state
	}

	w.mu.Lock()
	w.streak[key] = streak + len(pending)
	w.mu.Unlock()
}

// pendingConfirmations returns the call IDs of every adk_request_confirmation
// wrapper in sess's history that has no matching FunctionResponse yet --
// i.e. a genuinely unanswered confirmation, mirroring the same detection
// Botson-TUI's chat.go already does client-side (see its processEvents).
func pendingConfirmations(sess session.Session) []string {
	asked := map[string]bool{}
	answered := map[string]bool{}

	for evt := range sess.Events().All() {
		if evt.Content == nil {
			continue
		}
		for _, part := range evt.Content.Parts {
			switch {
			case part.FunctionCall != nil && part.FunctionCall.Name == toolconfirmation.FunctionCallName:
				asked[part.FunctionCall.ID] = true
			case part.FunctionResponse != nil && part.FunctionResponse.Name == toolconfirmation.FunctionCallName:
				answered[part.FunctionResponse.ID] = true
			}
		}
	}

	var pending []string
	for id := range asked {
		if !answered[id] {
			pending = append(pending, id)
		}
	}
	return pending
}

// The types below mirror the JSON shapes ADK's REST API actually expects on
// POST /api/run (see google.golang.org/adk/v2/server/adkrest, whose own
// request/response models live under an internal/ package this module can't
// import across module boundaries) -- verified against Botson-TUI's own
// hand-rolled mirror, itself checked against a real persisted session. Kept
// minimal: only the fields this package actually sends.
type runAgentRequest struct {
	AppName    string      `json:"appName"`
	UserID     string      `json:"userId"`
	SessionID  string      `json:"sessionId"`
	NewMessage wireContent `json:"newMessage"`
}

type wireContent struct {
	Role  string     `json:"role,omitempty"`
	Parts []wirePart `json:"parts"`
}

type wirePart struct {
	FunctionResponse *wireFunctionResponse `json:"functionResponse,omitempty"`
}

type wireFunctionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name,omitempty"`
	Response map[string]any `json:"response,omitempty"`
}

// autoApprove sends one run turn answering every pending confirmation id at
// once (ADK expects a batch answered together -- see AGENTS.md), marking
// each response with botsonAutoMode so any client rendering this session's
// history later can tell this apart from a human's own approval.
func (w *worker) autoApprove(ctx context.Context, sess session.Session, pending []string) error {
	parts := make([]wirePart, 0, len(pending))
	for _, id := range pending {
		parts = append(parts, wirePart{
			FunctionResponse: &wireFunctionResponse{
				ID:   id,
				Name: toolconfirmation.FunctionCallName,
				Response: map[string]any{
					"confirmed":      true,
					"botsonAutoMode": true,
				},
			},
		})
	}

	body, err := json.Marshal(runAgentRequest{
		AppName:    sess.AppName(),
		UserID:     sess.UserID(),
		SessionID:  sess.ID(),
		NewMessage: wireContent{Role: "user", Parts: parts},
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.baseURL+"/api/run", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.authToken)

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("automode: run request failed: status %d: %s", resp.StatusCode, respBody)
	}
	return nil
}
