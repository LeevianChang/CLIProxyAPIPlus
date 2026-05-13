package claude

import (
	"encoding/json"
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
