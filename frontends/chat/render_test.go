package chat

import (
	"testing"

	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"

	"botson/internal/networking/adkwire"
)

func TestRenderEvents_PlainText(t *testing.T) {
	events := []adkwire.Event{
		{Author: "chat:alice", Content: &genai.Content{Parts: []*genai.Part{{Text: "hi"}}}},
		{Author: "Agent Botson", Content: &genai.Content{Parts: []*genai.Part{{Text: "hello there"}}}},
	}

	lines, pending := renderEvents(events)

	if pending != nil {
		t.Fatalf("expected no pending confirmation, got %+v", pending)
	}
	want := []string{"chat:alice: hi", "Agent Botson: hello there"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("lines[%d] = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestRenderEvents_NonConfirmationToolCallAndResponse(t *testing.T) {
	events := []adkwire.Event{
		{Author: "Agent Botson", Content: &genai.Content{Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "c1", Name: "readFile"}},
		}}},
		{Author: "user", Content: &genai.Content{Parts: []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{ID: "c1", Name: "readFile"}},
		}}},
	}

	lines, pending := renderEvents(events)

	if pending != nil {
		t.Fatalf("expected no pending confirmation, got %+v", pending)
	}
	want := []string{"Agent Botson: [tool call: readFile]", "user: [tool response: readFile]"}
	if len(lines) != len(want) || lines[0] != want[0] || lines[1] != want[1] {
		t.Errorf("lines = %v, want %v", lines, want)
	}
}

func TestRenderEvents_DetectsPendingConfirmation(t *testing.T) {
	events := []adkwire.Event{
		{Author: "Agent Botson", Content: &genai.Content{Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{
				ID:   "confirm1",
				Name: toolconfirmation.FunctionCallName,
				Args: map[string]any{
					"toolConfirmation": map[string]any{"hint": "Approve writeFile?"},
					"originalFunctionCall": map[string]any{
						"id": "call0", "name": "writeFile", "args": map[string]any{"filePath": "x.txt"},
					},
				},
			}},
		}}},
	}

	lines, pending := renderEvents(events)

	if len(lines) != 0 {
		t.Errorf("expected no rendered text lines for a bare confirmation event, got %v", lines)
	}
	if pending == nil {
		t.Fatal("expected a pending confirmation, got nil")
	}
	if pending.callID != "confirm1" {
		t.Errorf("pending.callID = %q, want %q", pending.callID, "confirm1")
	}
	if pending.hint != "Approve writeFile?" {
		t.Errorf("pending.hint = %q, want %q", pending.hint, "Approve writeFile?")
	}
}

func TestRenderEvents_MissingHintFallsBackToDefault(t *testing.T) {
	events := []adkwire.Event{
		{Author: "Agent Botson", Content: &genai.Content{Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{
				ID:   "confirm1",
				Name: toolconfirmation.FunctionCallName,
				Args: map[string]any{},
			}},
		}}},
	}

	_, pending := renderEvents(events)

	if pending == nil {
		t.Fatal("expected a pending confirmation, got nil")
	}
	if pending.hint == "" {
		t.Error("expected a non-empty fallback hint")
	}
}

func TestRenderEvents_SkipsEmptyContentEvents(t *testing.T) {
	events := []adkwire.Event{
		{Author: "system", Content: nil},
		{Author: "Agent Botson", Content: &genai.Content{Parts: nil}},
	}

	lines, pending := renderEvents(events)

	if len(lines) != 0 {
		t.Errorf("expected no lines for content-less events, got %v", lines)
	}
	if pending != nil {
		t.Errorf("expected no pending confirmation, got %+v", pending)
	}
}
