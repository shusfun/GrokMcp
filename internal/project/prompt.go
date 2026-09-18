package project

import (
	"strings"

	"grokmcp/internal/protocol"
)

const (
	placeholderGoal        = "{goal}"
	placeholderConstraints = "{constraints}"
	placeholderAcceptance  = "{acceptance}"
	placeholderName        = "{name}"
	placeholderRoot        = "{root}"
	defaultGoal            = "（填写本次要完成的工作）"
	defaultConstraints     = "（填写约束；没有则写无）"
	defaultAcceptance      = "（填写验收标准）"
)

func BuiltinPromptTemplate() string {
	return strings.TrimSpace(`你是 Codex，正在通过 Grok Supervisor 与 Grok 协作。

协作约定：
- 重活交给 Grok 执行；你审计划和验收，不要自己做重实施。
- 用 grok_wait 等待边界状态，不要看过程流。
- 断连后继续原来的 Grok session，不要创建替换会话。
- 模型由用户配置，不要修改模型设置。

项目：{name}
路径：{root}

本次让 Grok 完成：
{goal}

约束：
{constraints}

验收：
{acceptance}`) + "\n"
}

func EffectiveTemplate(saved string) string {
	if strings.TrimSpace(saved) == "" {
		return BuiltinPromptTemplate()
	}
	return saved
}

func Generate(p protocol.Project, goal, constraints, acceptance string) protocol.PromptResult {
	tmpl := EffectiveTemplate(p.PromptTemplate)
	goal = displaySection(goal, defaultGoal)
	constraints = displaySection(constraints, defaultConstraints)
	acceptance = displaySection(acceptance, defaultAcceptance)
	text := tmpl
	text = strings.ReplaceAll(text, placeholderName, emptyDash(p.Name))
	text = strings.ReplaceAll(text, placeholderRoot, emptyDash(p.Root))
	text = strings.ReplaceAll(text, placeholderGoal, goal)
	text = strings.ReplaceAll(text, placeholderConstraints, constraints)
	text = strings.ReplaceAll(text, placeholderAcceptance, acceptance)
	return protocol.PromptResult{
		ProjectID:   p.ProjectID,
		Text:        text,
		Builtin:     BuiltinPromptTemplate(),
		Goal:        goal,
		Constraints: constraints,
		Acceptance:  acceptance,
	}
}

func displaySection(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}

func emptyDash(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "—"
	}
	return v
}
