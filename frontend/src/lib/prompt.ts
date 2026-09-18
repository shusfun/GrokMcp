const DEFAULT_GOAL = "（填写本次要完成的工作）";
const DEFAULT_CONSTRAINTS = "（填写约束；没有则写无）";
const DEFAULT_ACCEPTANCE = "（填写验收标准）";

export const builtinPromptTemplate = `你是 Codex，正在通过 Grok Supervisor 与 Grok 协作。

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
{acceptance}
`;

export function effectiveTemplate(saved: string | undefined, builtin = builtinPromptTemplate): string {
  return saved && saved.trim() ? saved : builtin;
}

export function generateDraft(
  template: string,
  ctx: { name: string; root: string; goal?: string; constraints?: string; acceptance?: string },
): string {
  const goal = ctx.goal?.trim() || DEFAULT_GOAL;
  const constraints = ctx.constraints?.trim() || DEFAULT_CONSTRAINTS;
  const acceptance = ctx.acceptance?.trim() || DEFAULT_ACCEPTANCE;
  const repls: [string, string][] = [
    ["{{project_path}}", ctx.root || "—"],
    ["{{name}}", ctx.name || "—"],
    ["{{root}}", ctx.root || "—"],
    ["{{goal}}", goal],
    ["{{constraints}}", constraints],
    ["{{acceptance}}", acceptance],
    ["{project_path}", ctx.root || "—"],
    ["{name}", ctx.name || "—"],
    ["{root}", ctx.root || "—"],
    ["{goal}", goal],
    ["{constraints}", constraints],
    ["{acceptance}", acceptance],
  ];
  let text = template;
  for (const [from, to] of repls) text = text.replaceAll(from, to);
  return text;
}

export function defaultTemplateToSave(template: string): string {
  return template;
}
