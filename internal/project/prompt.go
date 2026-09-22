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
- 用 grok_wait 等待边界，默认 300 秒；report_due 时每五分钟简报，携带返回 cursors 继续等待。不要通过 followup 催进度，不读取过程流。审批必须携带 approval_id 与请求、turn、版本绑定。review_required 时读取 grok_status(include_result=true) 的最终回答验收，不追加格式纠正消息。只有新的 boundary 才审批或验收；明确重新规划用 replan=true。
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
	text := applyTemplate(tmpl, p.Name, p.Root, goal, constraints, acceptance)
	return protocol.PromptResult{
		ProjectID:   p.ProjectID,
		Text:        text,
		Template:    tmpl,
		Builtin:     BuiltinPromptTemplate(),
		Goal:        goal,
		Constraints: constraints,
		Acceptance:  acceptance,
	}
}

func applyTemplate(tmpl, name, root, goal, constraints, acceptance string) string {
	repls := [][2]string{
		{"{{project_path}}", emptyDash(root)},
		{"{{name}}", emptyDash(name)},
		{"{{root}}", emptyDash(root)},
		{"{{goal}}", goal},
		{"{{constraints}}", constraints},
		{"{{acceptance}}", acceptance},
		{"{project_path}", emptyDash(root)},
		{placeholderName, emptyDash(name)},
		{placeholderRoot, emptyDash(root)},
		{placeholderGoal, goal},
		{placeholderConstraints, constraints},
		{placeholderAcceptance, acceptance},
	}
	text := tmpl
	for _, pair := range repls {
		text = strings.ReplaceAll(text, pair[0], pair[1])
	}
	return text
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
