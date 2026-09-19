import { describe, expect, it } from "vitest";
import { mockClient } from "./mock";
import { ccswitchOpenedPending } from "../lib/mcp-install";

describe("mock MCP install flow", () => {
  it("returns copyable config without claiming install", async () => {
    const cfg = await mockClient.mcpConfig();
    expect(cfg.server_id).toBe("grok_supervisor");
    expect(cfg.json).toContain("mcpServers");
    expect(cfg.deep_link).toContain("ccswitch://v1/import");
    expect(cfg.codex_add_command).toContain("codex mcp add grok_supervisor");
  });

  it("open CC-Switch import is pending confirmation, not live", async () => {
    const res = await mockClient.openCCSwitchMCPImport();
    expect(ccswitchOpenedPending(res)).toBe(true);
    expect(res.live_effective).toBe(false);
    expect(res.message).not.toMatch(/已安装|已生效/);
  });

  it("status keeps ccswitch and codex layers separate", async () => {
    const st = await mockClient.mcpStatus();
    expect(st.ccswitch.registered).toBe(false);
    expect(st.codex.live_visible).toBe(false);
    expect(st.generated.json).toBeTruthy();
  });
});
