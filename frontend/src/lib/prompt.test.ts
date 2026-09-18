import { describe, expect, it } from "vitest";
import { builtinPromptTemplate, defaultTemplateToSave, effectiveTemplate, generateDraft } from "./prompt";

describe("prompt template vs draft", () => {
  it("saves raw template with placeholders, not the generated draft", () => {
    const template = effectiveTemplate("path={root} do {goal}");
    const draft = generateDraft(template, { name: "n", root: "/tmp/p", goal: "修导入", constraints: "无", acceptance: "过测试" });
    expect(draft).toContain("/tmp/p");
    expect(draft).toContain("修导入");
    expect(draft).not.toContain("{goal}");
    expect(defaultTemplateToSave(template)).toBe("path={root} do {goal}");
    expect(defaultTemplateToSave(template)).not.toBe(draft);
  });

  it("generate after save still fills from saved placeholders", () => {
    const saved = defaultTemplateToSave("{{project_path}} {{goal}} {{constraints}} {{acceptance}}");
    const again = generateDraft(saved, { name: "n", root: "/repo", goal: "二次", constraints: "C", acceptance: "A" });
    expect(saved).toContain("{{goal}}");
    expect(again).toBe("/repo 二次 C A");
  });

  it("empty saved template falls back to builtin placeholders", () => {
    const tmpl = effectiveTemplate("", builtinPromptTemplate);
    expect(tmpl).toContain("{goal}");
    const draft = generateDraft(tmpl, { name: "demo", root: "/tmp/demo", goal: "G" });
    expect(draft).toContain("demo");
    expect(draft).toContain("G");
    expect(draft).not.toContain("{goal}");
  });
});
