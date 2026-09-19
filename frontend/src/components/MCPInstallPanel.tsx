import { useEffect, useState } from "react";
import { getClient } from "../api";
import type { MCPApplyResult, MCPInstallStatus } from "../api/client";
import { confirmAction } from "../lib/confirm";
import {
  applyNoteKind,
  canOpenDeepLink,
  ccswitchMode,
  copyJSONFor,
  layerLabels,
  twoPathsNote,
} from "../lib/mcp-install";
import { Button } from "./ui/button";

async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

export function MCPInstallPanel() {
  const client = getClient();
  const [status, setStatus] = useState<MCPInstallStatus | null>(null);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
  const [noteKind, setNoteKind] = useState<"ok" | "pending" | "warn">("ok");
  const [copied, setCopied] = useState("");
  const [busy, setBusy] = useState(false);

  const load = () => {
    setError("");
    void client.mcpStatus().then(setStatus).catch((e: Error) => {
      setError(e.message);
      void client.mcpConfig().then((generated) => {
        setStatus({
          generated,
          ccswitch: { detected: "read_error", registered: false, enabled_codex: false, needs_update: false, message: "无法探测状态" },
          codex: { cli_found: false, live_visible: false, enabled: false, command_match: false, timeouts_present: false, needs_update: false },
        });
      }).catch(() => undefined);
    });
  };

  useEffect(() => {
    load();
  }, [client]);

  const onCopy = (label: string, text: string) => {
    void copyText(text).then((ok) => setCopied(ok ? `已复制${label}` : "复制失败"));
  };

  const run = async (title: string, message: string, fn: () => Promise<MCPApplyResult>) => {
    const ok = await confirmAction(title, message);
    if (!ok) return;
    setBusy(true);
    setNote("");
    try {
      const res = await fn();
      setNoteKind(applyNoteKind(res));
      setNote(res.message + (res.next_step ? ` ${res.next_step}` : ""));
      load();
    } catch (e) {
      setNote(e instanceof Error ? e.message : "操作失败");
    } finally {
      setBusy(false);
    }
  };

  if (!status && !error) {
    return <p className="text-sm text-[var(--muted)]">检测 MCP 配置…</p>;
  }
  if (!status) {
    return <p className="text-sm text-[var(--fail)]">{error}</p>;
  }

  const bundle = status.generated;
  const mode = ccswitchMode(status.ccswitch);
  const labels = layerLabels(status);
  const jsonText = copyJSONFor(status.ccswitch, bundle);

  return (
    <div className="space-y-4">
      <p className="text-sm text-[var(--muted)]">{twoPathsNote}</p>
      <dl className="divide-y divide-[var(--line)] border-y border-[var(--line)] text-sm">
        <Row label="稳定 id" value={bundle.server_id} />
        <Row label="当前二进制" value={bundle.exe || "未知"} />
        <Row label="应用配置" value={labels.generated} />
        <Row label="CC-Switch 登记" value={labels.registered} />
        <Row label="CC-Switch enabled_codex" value={labels.enabled} />
        <Row label="Codex live" value={labels.live} />
      </dl>

      <div className="space-y-2 rounded-md border border-[var(--line)] bg-[var(--row)] p-3">
        <h3 className="text-sm font-semibold">CC-Switch</h3>
        <p className="text-xs text-[var(--muted)]">优先使用公开 deep link。打开后由 CC-Switch 自己导入，这里只显示待确认，不会写成已安装。</p>
        {mode === "import" ? (
          <>
            <div className="flex flex-wrap gap-2">
              <Button
                size="sm"
                disabled={busy || !canOpenDeepLink(status.ccswitch, bundle)}
                onClick={() => void run(
                  "用 CC-Switch 快速导入",
                  "将打开 CC-Switch deep link。不会改模型设置，也不会直接写 CC-Switch 数据库。导入是否成功请在 CC-Switch 确认后重新检测。",
                  () => client.openCCSwitchMCPImport(),
                )}
              >
                用 CC-Switch 快速导入
              </Button>
              <Button size="sm" variant="outline" onClick={() => onCopy(" JSON", bundle.json)}>复制 STDIO JSON</Button>
              <Button size="sm" variant="outline" onClick={() => onCopy(" deep link", bundle.deep_link)}>复制 deep link</Button>
            </div>
            {bundle.deep_link_supported === false ? (
              <p className="text-xs text-[var(--fail)]">当前平台未验证 CC-Switch deep link，请复制 JSON 手动导入。</p>
            ) : null}
          </>
        ) : null}
        {mode === "configured" ? (
          <p className="text-sm">已配置。不要再用 Codex Direct 添加重复项。</p>
        ) : null}
        {mode === "needs_update" ? (
          <>
            <p className="text-xs text-[var(--muted)]">已存在别名，deep link 只合并 apps，不能更新命令路径。请复制 JSON 并在 CC-Switch MCP 编辑页粘贴。</p>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" onClick={() => onCopy("更新 JSON", jsonText)}>复制更新 JSON</Button>
              <Button
                size="sm"
                variant="outline"
                disabled={busy}
                onClick={() => void run(
                  "打开 CC-Switch",
                  "将打开 CC-Switch 应用（不是导入链接）。请到 MCP 编辑页粘贴更新 JSON。",
                  () => client.openCCSwitchApp(),
                )}
              >
                打开 CC-Switch
              </Button>
            </div>
          </>
        ) : null}
        <Button size="sm" variant="ghost" disabled={busy} onClick={load}>重新检测</Button>
      </div>

      <div className="space-y-2 rounded-md border border-[var(--line)] bg-[var(--row)] p-3">
        <h3 className="text-sm font-semibold">Codex Direct</h3>
        <p className="text-xs text-[var(--muted)]">仅在不用 CC-Switch 管理 MCP 时使用。通过已安装的 Codex CLI 添加，不手改模型配置。</p>
        <div className="flex flex-wrap gap-2">
          <Button
            size="sm"
            disabled={busy}
            onClick={() => void run(
              status.codex.live_visible ? "更新到 Codex" : "添加/更新到 Codex",
              "将调用 codex mcp add 或在无法无损更新时只提供可复制配置。不会修改模型、provider 或认证。",
              () => client.addMCPToCodex(),
            )}
          >
            {status.codex.live_visible ? "更新到 Codex" : "添加/更新到 Codex"}
          </Button>
          <Button size="sm" variant="outline" onClick={() => onCopy("命令", bundle.codex_add_command)}>复制 add 命令</Button>
          <Button size="sm" variant="outline" onClick={() => onCopy(" TOML", bundle.toml)}>复制 TOML</Button>
        </div>
        {status.codex.live_visible && status.codex.command_match && !status.codex.enabled ? (
          <p className="text-xs text-[var(--muted)]">该 MCP 已存在但被禁用，请在 Codex 配置或 MCP 管理入口中启用。不会自动 remove+add。</p>
        ) : null}
        {status.codex.live_visible && status.codex.needs_update && !status.codex.command_match ? (
          <p className="text-xs text-[var(--muted)]">若已有 timeout/env，不会自动 remove，以免损坏配置。</p>
        ) : null}
      </div>

      {copied ? <p className="text-sm text-[var(--accent)]">{copied}</p> : null}
      {note ? <p className={`text-sm ${noteKind === "warn" ? "text-[var(--fail)]" : "text-[var(--accent)]"}`}>{note}</p> : null}
      {error ? <p className="text-sm text-[var(--fail)]">{error}</p> : null}
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid grid-cols-3 gap-4 py-2.5">
      <dt className="text-[var(--muted)]">{label}</dt>
      <dd className="col-span-2 break-all">{value}</dd>
    </div>
  );
}
