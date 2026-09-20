import { describe, expect, it } from "vitest";
import type { MCPApplyResult, MCPCCSwitchStatus, MCPCodexStatus, MCPConfigBundle } from "../api/client";
import {
  applyLooksLiveEffective,
  applyNoteKind,
  canOpenDeepLink,
  ccswitchMode,
  ccswitchOpenedPending,
  copyJSONFor,
  layerLabels,
  twoPathsNote,
} from "./mcp-install";

const bundle: MCPConfigBundle = {
  server_id: "grok_supervisor",
  exe: "/Applications/Grok Supervisor.app/Contents/MacOS/GrokMcp",
  args: ["mcp"],
  startup_timeout_sec: 30,
  tool_timeout_sec: 21600,
  json: `{"mcpServers":{"grok_supervisor":{}}}`,
  update_json: `{"mcpServers":{"grok_supervisor":{}}}`,
  deep_link: "ccswitch://v1/import?resource=mcp&apps=codex&config=abc",
  codex_add_command: "codex mcp add grok_supervisor -- x mcp",
  toml: "[mcp_servers.grok_supervisor]",
  deep_link_supported: true,
};

function cc(partial: Partial<MCPCCSwitchStatus>): MCPCCSwitchStatus {
  return { detected: "detected", registered: false, enabled_codex: false, needs_update: false, ...partial };
}

describe("mcp install ui logic", () => {
  it("classifies ccswitch modes", () => {
    expect(ccswitchMode(cc({}))).toBe("import");
    expect(ccswitchMode(cc({ registered: true }))).toBe("configured");
    expect(ccswitchMode(cc({ registered: true, needs_update: true }))).toBe("needs_update");
  });

  it("allows deep link for new import and legacy-name migration", () => {
    expect(canOpenDeepLink(cc({}), bundle)).toBe(true);
    expect(canOpenDeepLink(cc({ registered: true, needs_update: true, legacy_id: "Grok Supervisor" }), bundle)).toBe(true);
    expect(canOpenDeepLink(cc({ registered: true, needs_update: true }), bundle)).toBe(false);
    expect(canOpenDeepLink(cc({ registered: true }), bundle)).toBe(false);
    expect(canOpenDeepLink(cc({}), { ...bundle, deep_link_supported: false })).toBe(false);
  });

  it("does not treat opened deep link as installed or live", () => {
    const res: MCPApplyResult = {
      ok: true,
      action: "pending_user_confirmation",
      target: "ccswitch",
      live_effective: false,
      message: "已打开 CC-Switch，待你确认导入",
    };
    expect(ccswitchOpenedPending(res)).toBe(true);
    expect(applyLooksLiveEffective(res)).toBe(false);
  });

  it("copies update json when needs_update", () => {
    expect(copyJSONFor(cc({ registered: true, needs_update: true }), bundle)).toContain("grok_supervisor");
    expect(copyJSONFor(cc({}), bundle)).toContain("grok_supervisor");
  });

  it("keeps status layers distinct", () => {
    const labels = layerLabels({
      ccswitch: cc({ registered: true, enabled_codex: true, legacy_id: "Grok Supervisor" }),
      codex: { cli_found: true, live_visible: false, enabled: false, command_match: false, timeouts_present: false, needs_update: false } satisfies MCPCodexStatus,
    });
    expect(labels.registered).toContain("旧名称无效");
    expect(labels.enabled).toContain("已启用");
    expect(labels.live).toContain("未见");
  });

  it("marks the space-containing legacy Codex id as a desktop load failure", () => {
    const labels = layerLabels({
      ccswitch: cc({}),
      codex: { cli_found: true, live_visible: true, live_name: "Grok Supervisor", enabled: true, command_match: true, timeouts_present: true, needs_update: true },
    });
    expect(labels.live).toContain("加载失败");
  });

  it("does not treat disabled matching Codex as live effective", () => {
    const res: MCPApplyResult = {
      ok: true,
      action: "needs_manual",
      target: "codex",
      live_effective: false,
      message: "该 MCP 已存在但被禁用（grok_supervisor）",
      next_step: "请在 Codex 配置中启用，或通过 Codex 的 MCP 管理入口启用。",
    };
    expect(applyLooksLiveEffective(res)).toBe(false);
    expect(applyNoteKind(res)).toBe("warn");
    const labels = layerLabels({
      ccswitch: cc({}),
      codex: { cli_found: true, live_visible: true, live_name: "grok_supervisor", enabled: false, command_match: true, timeouts_present: true, needs_update: false },
    });
    expect(labels.live).toContain("被禁用");
  });

  it("states the two paths are exclusive", () => {
    expect(twoPathsNote).toContain("二选一");
    expect(twoPathsNote).toContain("不要重复添加");
  });
});
