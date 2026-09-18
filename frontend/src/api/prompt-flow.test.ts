import { describe, expect, it } from "vitest";
import { mockClient } from "./mock";

describe("mock prompt save then generate", () => {
  it("keeps raw placeholders on the saved template", async () => {
    const saved = await mockClient.savePrompt("proj-suiyuan", "do {goal} at {root}");
    expect(saved.prompt_template).toBe("do {goal} at {root}");
    const out = await mockClient.generatePrompt("proj-suiyuan", "二次", "C", "A");
    expect(out.template).toBe("do {goal} at {root}");
    expect(out.text).toContain("二次");
    expect(out.text).toContain("/tmp/suiyuan");
    expect(out.text).not.toContain("{goal}");
  });
});
