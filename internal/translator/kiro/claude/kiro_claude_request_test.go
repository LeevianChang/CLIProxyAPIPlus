package claude

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildKiroPayloadDropsHistoryImagesKeepsCurrentImages(t *testing.T) {
	input := []byte(`{
  "model":"claude-sonnet-4.6",
  "messages":[
    {"role":"user","content":[{"type":"text","text":"old image"},{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"oldbase64"}}]},
    {"role":"assistant","content":"ok"},
    {"role":"user","content":[{"type":"text","text":"new image"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"newbase64"}}]}
  ]
}`)

	out, _ := BuildKiroPayload(input, "claude-sonnet-4.6", "", "AI_EDITOR", false, false, nil, nil)
	var payload KiroPayload
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.ConversationState.History) == 0 || payload.ConversationState.History[0].UserInputMessage == nil {
		t.Fatalf("expected history user message, payload=%s", out)
	}
	if got := len(payload.ConversationState.History[0].UserInputMessage.Images); got != 0 {
		t.Fatalf("history images=%d, want 0", got)
	}
	if got := len(payload.ConversationState.CurrentMessage.UserInputMessage.Images); got != 1 {
		t.Fatalf("current images=%d, want 1", got)
	}
}

func TestBuildKiroPayloadForwardsCurrentDocument(t *testing.T) {
	input := []byte(`{
  "model":"claude-sonnet-4.6",
  "messages":[
    {"role":"user","content":[{"type":"text","text":"old document"},{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"oldpdf"},"title":"old.pdf"}]},
    {"role":"assistant","content":"ok"},
    {"role":"user","content":[{"type":"text","text":"summarize"},{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"newpdf"},"title":"new.pdf"}]}
  ]
}`)

	out, _ := BuildKiroPayload(input, "claude-sonnet-4.6", "", "AI_EDITOR", false, false, nil, nil)
	var payload KiroPayload
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.ConversationState.History[0].UserInputMessage.Documents) != 0 {
		t.Fatalf("history documents should be empty")
	}
	docs := payload.ConversationState.CurrentMessage.UserInputMessage.Documents
	if len(docs) != 1 {
		t.Fatalf("current documents length=%d, want 1", len(docs))
	}
	doc := docs[0]
	if doc.Format != "pdf" || doc.Name != "new.pdf" || doc.Source.Bytes != "newpdf" {
		t.Fatalf("unexpected document: %+v", doc)
	}
}

func TestBuildKiroPayloadInjectsIdentityPolicy(t *testing.T) {
	input := []byte(`{
  "model":"claude-sonnet-4.6",
  "system":"You are helpful.",
  "messages":[{"role":"user","content":"hello"}]
}`)

	out, _ := BuildKiroPayload(input, "claude-sonnet-4.6", "", "AI_EDITOR", false, false, nil, nil)
	var payload KiroPayload
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	content := payload.ConversationState.CurrentMessage.UserInputMessage.Content
	if !strings.Contains(content, "You are helpful.") {
		t.Fatalf("expected original system prompt, got %q", content)
	}
	if !strings.Contains(content, "Do not introduce yourself or state which assistant") {
		t.Fatalf("expected identity policy, got %q", content)
	}
}
