package chat

import (
	"fmt"
	"strings"

	"google.golang.org/adk/v2/tool/toolconfirmation"

	"botson/internal/networking/adkwire"
)

// renderEvents turns a turn's events into scrollback lines, and reports a
// pendingConfirmation if the turn paused on an adk_request_confirmation
// call -- see AGENTS.md's "HITL confirmation wire protocol" for the wire
// sequence this is reading.
//
// Non-confirmation tool calls/responses are rendered as bracketed markers
// (e.g. "[tool call: writeFile]"), the same convention
// internal/management's SessionEventSummary already uses for the dashboard
// view, so a tool-heavy turn doesn't dump raw JSON into the chat scrollback.
func renderEvents(events []adkwire.Event) (lines []string, pending *pendingConfirmation) {
	for _, ev := range events {
		if ev.Content == nil {
			continue
		}
		var text strings.Builder
		for _, part := range ev.Content.Parts {
			switch {
			case part.Text != "":
				text.WriteString(part.Text)
			case part.FunctionCall != nil && part.FunctionCall.Name == toolconfirmation.FunctionCallName:
				hint := ""
				if tc, ok := part.FunctionCall.Args["toolConfirmation"].(map[string]any); ok {
					hint, _ = tc["hint"].(string)
				}
				if hint == "" {
					hint = "Approve this action?"
				}
				pending = &pendingConfirmation{callID: part.FunctionCall.ID, hint: hint}
			case part.FunctionCall != nil:
				text.WriteString(fmt.Sprintf("[tool call: %s]", part.FunctionCall.Name))
			case part.FunctionResponse != nil && part.FunctionResponse.Name != toolconfirmation.FunctionCallName:
				text.WriteString(fmt.Sprintf("[tool response: %s]", part.FunctionResponse.Name))
			}
		}
		if text.Len() > 0 {
			lines = append(lines, fmt.Sprintf("%s: %s", ev.Author, text.String()))
		}
	}
	return lines, pending
}
