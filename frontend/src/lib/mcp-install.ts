import type { MCPApplyResult, MCPCCSwitchStatus, MCPCodexStatus, MCPConfigBundle } from "../api/client";

export type CCSwitchMode = "import" | "configured" | "needs_update";

export function ccswitchMode(st: MCPCCSwitchStatus): CCSwitchMode {
  if (st.registered && st.needs_update) return "needs_update";
  if (st.registered) return "configured";
  return "import";
}

export function canOpenDeepLink(st: MCPCCSwitchStatus, bundle: MCPConfigBundle): boolean {
  return ccswitchMode(st) === "import" && bundle.deep_link_supported !== false;
}

export function applyLooksLiveEffective(res: MCPApplyResult): boolean {
  if (!res.live_effective) return false;
  return res.action === "created" || res.action === "updated" || res.action === "unchanged";
}

export function applyNoteKind(res: MCPApplyResult): "ok" | "pending" | "warn" {
  if (res.action === "pending_user_confirmation") return "pending";
  if (applyLooksLiveEffective(res)) return "ok";
  return "warn";
}

export function ccswitchOpenedPending(res: MCPApplyResult): boolean {
  return res.action === "pending_user_confirmation" && res.target === "ccswitch" && !res.live_effective;
}

export function copyJSONFor(st: MCPCCSwitchStatus, bundle: MCPConfigBundle): string {
  if (ccswitchMode(st) === "needs_update" && bundle.update_json) return bundle.update_json;
  return bundle.json;
}

export function layerLabels(st: { ccswitch: MCPCCSwitchStatus; codex: MCPCodexStatus }): {
  generated: string;
  registered: string;
  enabled: string;
  live: string;
} {
  const cc = st.ccswitch;
  const cx = st.codex;
  let registered = "未登记";
  if (cc.detected === "missing") registered = "未检测到 CC-Switch";
  else if (cc.detected === "read_error" || cc.detected === "locked" || cc.detected === "unsupported_schema") registered = cc.message || "无法探测";
  else if (cc.registered) registered = cc.legacy_id ? `已登记（${cc.legacy_id}）` : "已登记";
  return {
    generated: "当前应用配置",
    registered,
    enabled: cc.enabled_codex ? "CC-Switch 已启用 Codex" : "CC-Switch 未启用 Codex",
    live: !cx.cli_found ? "未找到 Codex CLI" : cx.live_visible ? (cx.enabled ? "Codex live 可见且启用" : "Codex 已存在但被禁用") : "Codex live 未见",
  };
}

export const twoPathsNote = "两条路径二选一：若用 CC-Switch 管理 MCP，请走 deep link；不用 CC-Switch 再走 Codex Direct。不要重复添加。";
