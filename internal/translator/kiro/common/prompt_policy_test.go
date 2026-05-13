package common

import (
	"strings"
	"testing"
)

func TestAppendChunkedToolSystemPolicy(t *testing.T) {
	out := AppendChunkedToolSystemPolicy("You are helpful.")
	if !strings.Contains(out, "You are helpful.") {
		t.Fatalf("expected original prompt, got %q", out)
	}
	if !strings.Contains(out, KiroChunkedToolSystemPolicy) {
		t.Fatalf("expected chunked policy, got %q", out)
	}

	again := AppendChunkedToolSystemPolicy(out)
	if strings.Count(again, KiroChunkedToolSystemPolicy) != 1 {
		t.Fatalf("expected policy once, got %q", again)
	}
}

func TestAppendChunkedToolDescriptionPolicy(t *testing.T) {
	writeDesc := AppendChunkedToolDescriptionPolicy("Write", "Writes files")
	if !strings.Contains(writeDesc, KiroWriteToolDescriptionSuffix) {
		t.Fatalf("expected write suffix, got %q", writeDesc)
	}

	editDesc := AppendChunkedToolDescriptionPolicy("replace_in_file", "Edits files")
	if !strings.Contains(editDesc, KiroEditToolDescriptionSuffix) {
		t.Fatalf("expected edit suffix, got %q", editDesc)
	}

	readDesc := AppendChunkedToolDescriptionPolicy("Read", "Reads files")
	if readDesc != "Reads files" {
		t.Fatalf("expected read description unchanged, got %q", readDesc)
	}
}
