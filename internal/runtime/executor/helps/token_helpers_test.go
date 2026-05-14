package helps

import (
	"strings"
	"testing"

	"github.com/tiktoken-go/tokenizer"
)

func TestCountClaudeChatTokensSkipsDocumentBase64Payload(t *testing.T) {
	enc, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		t.Fatalf("tokenizer.Get() error: %v", err)
	}

	largePDF := strings.Repeat("A", 120000)
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"summarize this"},{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"` + largePDF + `"},"title":"sample.pdf"}]}]}`)

	count, err := CountClaudeChatTokens(enc, payload)
	if err != nil {
		t.Fatalf("CountClaudeChatTokens() error: %v", err)
	}
	if count > 200 {
		t.Fatalf("token count = %d, want document metadata only", count)
	}
}
