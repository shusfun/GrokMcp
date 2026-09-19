package integration

import (
	"strconv"
	"strings"
)

func tomlQuote(s string) string {
	return strconv.Quote(s)
}

func posixQuote(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\n\"'\\$`") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

func powershellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
