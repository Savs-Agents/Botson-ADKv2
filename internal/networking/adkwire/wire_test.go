package adkwire

import (
	"encoding/json"
	"testing"

	"google.golang.org/genai"
)

// TestRunAgentRequest_MarshalShape guards the exact wire shape ADK's real
// server/adkrest/internal/models.RunAgentRequest expects -- confirmed
// directly against that vendored source, not assumed. A regression here
// (e.g. an accidental json tag rename) would silently break every /api/run
// caller in this module.
func TestRunAgentRequest_MarshalShape(t *testing.T) {
	req := RunAgentRequest{
		AppName:   "Agent Botson",
		UserID:    "chat:alice",
		SessionID: "abc-123",
		NewMessage: genai.Content{
			Role:  "user",
			Parts: []*genai.Part{{Text: "hello"}},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}

	for _, key := range []string{"appName", "userId", "sessionId", "newMessage"} {
		if _, ok := got[key]; !ok {
			t.Errorf("expected key %q in marshaled JSON, got %s", key, data)
		}
	}

	newMessage, ok := got["newMessage"].(map[string]any)
	if !ok {
		t.Fatalf("newMessage is not an object: %s", data)
	}
	if newMessage["role"] != "user" {
		t.Errorf("newMessage.role = %v, want %q", newMessage["role"], "user")
	}
	parts, ok := newMessage["parts"].([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("newMessage.parts = %v, want a single-element array", newMessage["parts"])
	}
	part, ok := parts[0].(map[string]any)
	if !ok || part["text"] != "hello" {
		t.Errorf("newMessage.parts[0].text = %v, want %q", parts[0], "hello")
	}
}

// TestEvent_UnmarshalFromRealResponseShape confirms Event correctly decodes
// a payload shaped like ADK's real /api/run response -- including ignoring
// the many fields models.Event carries that this type doesn't need.
func TestEvent_UnmarshalFromRealResponseShape(t *testing.T) {
	body := `[
		{"id":"ev1","invocationId":"inv1","author":"user","content":{"role":"user","parts":[{"text":"hi"}]}},
		{"id":"ev2","invocationId":"inv1","author":"Agent Botson","content":{"role":"model","parts":[{"text":"hello there"}]},"actions":{"stateDelta":{},"artifactDelta":{}}}
	]`

	var events []Event
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Author != "user" {
		t.Errorf("events[0].Author = %q, want %q", events[0].Author, "user")
	}
	if events[1].Author != "Agent Botson" {
		t.Errorf("events[1].Author = %q, want %q", events[1].Author, "Agent Botson")
	}
	if events[1].Content == nil || len(events[1].Content.Parts) != 1 || events[1].Content.Parts[0].Text != "hello there" {
		t.Errorf("events[1].Content = %+v, want a single text part %q", events[1].Content, "hello there")
	}
}

// TestEvent_UnmarshalFunctionCall confirms a functionCall part (the shape
// used for both real tool calls and the adk_request_confirmation wrapper)
// round-trips correctly, since that's the part frontends/chat and
// internal/automode both key their HITL handling off.
func TestEvent_UnmarshalFunctionCall(t *testing.T) {
	body := `{"author":"Agent Botson","content":{"role":"model","parts":[
		{"functionCall":{"id":"call1","name":"adk_request_confirmation","args":{"toolConfirmation":{"hint":"Approve writeFile?"},"originalFunctionCall":{"id":"call0","name":"writeFile","args":{"filePath":"x.txt"}}}}}
	]}}`

	var ev Event
	if err := json.Unmarshal([]byte(body), &ev); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if ev.Content == nil || len(ev.Content.Parts) != 1 {
		t.Fatalf("Content = %+v, want a single part", ev.Content)
	}
	fc := ev.Content.Parts[0].FunctionCall
	if fc == nil || fc.Name != "adk_request_confirmation" || fc.ID != "call1" {
		t.Errorf("FunctionCall = %+v, want name=adk_request_confirmation id=call1", fc)
	}
}
