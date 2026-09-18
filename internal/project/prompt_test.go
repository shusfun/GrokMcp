package project

import (
	"strings"
	"testing"

	"grokmcp/internal/protocol"
)

func TestGenerateBuiltinSections(t *testing.T) {
	p := protocol.Project{ProjectID: "p1", Name: "demo", Root: "/tmp/demo"}
	got := Generate(p, "修导入", "不改模型", "测试通过")
	if got.Builtin == "" || got.Text == "" {
		t.Fatal("empty")
	}
	for _, need := range []string{"重活", "grok_wait", "过程", "session", "模型由用户", "修导入", "不改模型", "测试通过", "demo", "/tmp/demo"} {
		if !strings.Contains(got.Text, need) {
			t.Fatalf("missing %q in %s", need, got.Text)
		}
	}
	if strings.Contains(got.Text, "{goal}") {
		t.Fatal("unreplaced placeholder")
	}
}

func TestGenerateUsesSavedTemplate(t *testing.T) {
	p := protocol.Project{ProjectID: "p1", Name: "n", Root: "/r", PromptTemplate: "ONLY {goal}"}
	got := Generate(p, "X", "", "")
	if strings.TrimSpace(got.Text) != "ONLY X" && !strings.Contains(got.Text, "ONLY X") {
		t.Fatalf("%q", got.Text)
	}
	if got.Template != "ONLY {goal}" {
		t.Fatalf("template %q", got.Template)
	}
	if got.Builtin == p.PromptTemplate {
		t.Fatal("builtin should stay built-in")
	}
}

func TestGenerateKeepsRawTemplateWithDoubleBracePlaceholders(t *testing.T) {
	tmpl := "path={{project_path}} goal={{goal}} c={{constraints}} a={{acceptance}}"
	p := protocol.Project{ProjectID: "p1", Name: "n", Root: "/tmp/demo", PromptTemplate: tmpl}
	got := Generate(p, "G", "C", "A")
	if got.Template != tmpl {
		t.Fatalf("template mutated %q", got.Template)
	}
	if strings.Contains(got.Text, "{{") || strings.Contains(got.Text, "{goal}") {
		t.Fatalf("draft still has placeholders %q", got.Text)
	}
	if !strings.Contains(got.Text, "/tmp/demo") || !strings.Contains(got.Text, "G") {
		t.Fatalf("draft %q", got.Text)
	}
}
