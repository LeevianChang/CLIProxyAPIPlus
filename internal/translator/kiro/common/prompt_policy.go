package common

import "strings"

func AppendChunkedToolSystemPolicy(systemPrompt string) string {
	return appendSystemPolicy(systemPrompt, KiroChunkedToolSystemPolicy)
}

func AppendIdentitySystemPolicy(systemPrompt string) string {
	return appendSystemPolicy(systemPrompt, KiroIdentitySystemPolicy)
}

func appendSystemPolicy(systemPrompt, policy string) string {
	systemPrompt = strings.TrimSpace(systemPrompt)
	if strings.Contains(systemPrompt, policy) {
		return systemPrompt
	}
	if systemPrompt == "" {
		return policy
	}
	return systemPrompt + "\n" + policy
}

func AppendChunkedToolDescriptionPolicy(name, description string) string {
	suffix := chunkedToolDescriptionSuffix(name)
	if suffix == "" || strings.Contains(description, suffix) {
		return description
	}
	description = strings.TrimSpace(description)
	if description == "" {
		return suffix
	}
	return description + "\n" + suffix
}

func chunkedToolDescriptionSuffix(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.TrimPrefix(normalized, "mcp__")
	switch normalized {
	case "write", "write_to_file", "fswrite", "create_file":
		return KiroWriteToolDescriptionSuffix
	case "edit", "replace_in_file", "apply_diff", "str_replace", "fsreplace":
		return KiroEditToolDescriptionSuffix
	}
	if strings.Contains(normalized, "write") {
		return KiroWriteToolDescriptionSuffix
	}
	if strings.Contains(normalized, "edit") || strings.Contains(normalized, "replace") || strings.Contains(normalized, "diff") {
		return KiroEditToolDescriptionSuffix
	}
	return ""
}
