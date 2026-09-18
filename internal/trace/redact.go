package trace

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode/utf8"
)

var credRE = regexp.MustCompile(`(?i)((?:authorization|token|cookie|api[_-]?key|x-api-key)\s*[:=]\s*(?:bearer\s+)?)(\S+)`)

func RedactString(s string) string {
	return credRE.ReplaceAllString(s, "${1}<redacted>")
}

func Digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func PromptFields(text string, payloads bool) map[string]any {
	summary := strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if utf8.RuneCountInString(summary) > 80 {
		summary = string([]rune(summary)[:80]) + "…"
	}
	fields := map[string]any{
		"prompt_length":  len(text),
		"prompt_hash":    Digest(text),
		"prompt_summary": RedactString(summary),
	}
	if payloads {
		fields["prompt"] = RedactString(text)
	}
	return fields
}

func SanitizeFields(fields map[string]any, payloads bool) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		lk := strings.ToLower(k)
		if !payloads && (lk == "prompt" || lk == "content" || lk == "payload" || lk == "body" || lk == "text") {
			if s, ok := v.(string); ok {
				out[k+"_length"] = len(s)
				out[k+"_hash"] = Digest(s)
				continue
			}
		}
		out[k] = sanitizeValue(k, v, payloads)
	}
	return out
}

func sanitizeValue(key string, v any, payloads bool) any {
	switch t := v.(type) {
	case string:
		s := RedactString(t)
		lk := strings.ToLower(key)
		if !payloads && (lk == "content" || lk == "file" || lk == "data") && len(s) > 200 {
			return map[string]any{"length": len(t), "hash": Digest(t)}
		}
		return s
	case map[string]any:
		return SanitizeFields(t, payloads)
	default:
		return v
	}
}
