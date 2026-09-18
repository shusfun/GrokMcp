import { describe, expect, it } from "vitest";
import { toggleResolved } from "./theme";

describe("theme", () => {
  it("toggles resolved light and dark", () => {
    expect(toggleResolved("dark")).toBe("light");
    expect(toggleResolved("light")).toBe("dark");
  });
});
