package artifact

import (
	"context"
	"testing"

	"google.golang.org/adk/v2/artifact"
	"google.golang.org/genai"
)

func newTestService(t *testing.T) *LocalFileService {
	t.Helper()
	svc, err := NewLocalFileService(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalFileService: %v", err)
	}
	return svc
}

func TestSanitizeSegment(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid simple", "Agent Botson", false},
		{"valid with dash and underscore", "chat-alice_1", false},
		{"empty", "", true},
		{"dot", ".", true},
		{"dotdot", "..", true},
		{"forward slash", "a/b", true},
		{"backslash", "a\\b", true},
		{"colon (the real Windows bug)", "chat:alice", true},
		{"windows reserved char less-than", "a<b", true},
		{"windows reserved char pipe", "a|b", true},
		{"windows reserved char question mark", "a?b", true},
		{"windows reserved char asterisk", "a*b", true},
		{"windows reserved char quote", `a"b`, true},
		{"control character", "a\x00b", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeSegment("field", tt.value)
			if tt.wantErr && err == nil {
				t.Errorf("sanitizeSegment(%q) = nil, want an error", tt.value)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("sanitizeSegment(%q) = %v, want nil", tt.value, err)
			}
		})
	}
}

func TestSave_RejectsUnsafeSegments(t *testing.T) {
	svc := newTestService(t)
	part := genai.NewPartFromText("hello")

	tests := []struct {
		name      string
		appName   string
		userID    string
		sessionID string
		fileName  string
	}{
		{"colon in userID (the real Windows bug)", "app", "chat:alice", "sess-1", "file.txt"},
		{"path traversal in fileName", "app", "user", "sess-1", "../../etc/passwd"},
		{"path traversal in sessionID", "app", "user", "..", "file.txt"},
		{"separator in appName", "a/b", "user", "sess-1", "file.txt"},
		{"backslash in fileName", "app", "user", "sess-1", "a\\b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Save(context.Background(), &artifact.SaveRequest{
				AppName:   tt.appName,
				UserID:    tt.userID,
				SessionID: tt.sessionID,
				FileName:  tt.fileName,
				Part:      part,
			})
			if err == nil {
				t.Fatalf("Save(%+v) = nil error, want a sanitization error", tt)
			}
		})
	}
}

func TestSave_AllowsSafeSegmentsAndRoundTripsThroughLoad(t *testing.T) {
	svc := newTestService(t)
	part := genai.NewPartFromText("hello world")

	saveResp, err := svc.Save(context.Background(), &artifact.SaveRequest{
		AppName:   "Agent Botson",
		UserID:    "chat-alice",
		SessionID: "sess-1",
		FileName:  "notes.txt",
		Part:      part,
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saveResp.Version != 1 {
		t.Errorf("Version = %d, want 1", saveResp.Version)
	}

	loadResp, err := svc.Load(context.Background(), &artifact.LoadRequest{
		AppName:   "Agent Botson",
		UserID:    "chat-alice",
		SessionID: "sess-1",
		FileName:  "notes.txt",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loadResp.Part.Text != "hello world" {
		t.Errorf("loaded text = %q, want %q", loadResp.Part.Text, "hello world")
	}
}

func TestList_RejectsUnsafeSegments(t *testing.T) {
	svc := newTestService(t)

	_, err := svc.List(context.Background(), &artifact.ListRequest{
		AppName:   "app",
		UserID:    "chat:alice",
		SessionID: "sess-1",
	})
	if err == nil {
		t.Fatal("List with a colon in userID = nil error, want a sanitization error")
	}
}
