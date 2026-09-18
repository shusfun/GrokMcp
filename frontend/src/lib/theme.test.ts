import { describe, expect, it } from "vitest";
import { cycleAppearance } from "./theme";

describe("theme", () => {
  it("cycles system, light and dark", () => {
    expect(cycleAppearance("system")).toBe("light");
    expect(cycleAppearance("light")).toBe("dark");
    expect(cycleAppearance("dark")).toBe("system");
  });
});
