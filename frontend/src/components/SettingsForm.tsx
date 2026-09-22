import type { Settings } from "../api/client";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Segmented } from "./ui/segmented";
import { Select } from "./ui/select";

export function SettingsForm({
  value,
  onChange,
  onSave,
  onTest,
}: {
  value: Settings;
  onChange: (s: Settings) => void;
  onSave: () => void;
  onTest: () => void;
}) {
  return (
    <form
      className="max-w-xl space-y-3"
      onSubmit={(e) => {
        e.preventDefault();
        onSave();
      }}
    >
      <label className="block text-sm text-[var(--muted)]">
        Grok 二进制
        <Input className="mt-1" value={value.grok_binary_path} onChange={(e) => onChange({ ...value, grok_binary_path: e.target.value })} placeholder="留空则自动发现" />
      </label>
      <label className="block text-sm text-[var(--muted)]">
        终端
        <Select
          className="mt-1 w-full"
          aria-label="终端"
          value={value.terminal_provider}
          options={[
            { value: "default", label: "平台默认" },
            { value: "custom", label: "自定义模板" },
          ]}
          onChange={(terminal_provider) => onChange({ ...value, terminal_provider })}
        />
      </label>
      <label className="block text-sm text-[var(--muted)]">
        终端命令模板
        <Input className="mt-1" value={value.terminal_command_template} onChange={(e) => onChange({ ...value, terminal_command_template: e.target.value })} placeholder="{command}（查看客户端）  {cwd}  {session_id}" />
      </label>
      <p className="text-sm text-[var(--muted)]">任务始终在后台运行。需要查看或交互时，主动打开对应任务的终端。</p>
      <div className="space-y-2">
        <p className="text-sm text-[var(--muted)]">调试模式</p>
        <Segmented
          aria-label="调试模式"
          value={value.debug_enabled ? "on" : "off"}
          options={[
            { value: "off", label: "关" },
            { value: "on", label: "开" },
          ]}
          onChange={(v) => onChange({ ...value, debug_enabled: v === "on" })}
        />
        {value.debug_enabled ? (
          <label className="flex items-center gap-2 text-sm text-[var(--muted)]">
            <input
              type="checkbox"
              checked={Boolean(value.debug_payloads)}
              onChange={(e) => onChange({ ...value, debug_payloads: e.target.checked })}
            />
            记录脱敏后的 payload
          </label>
        ) : null}
      </div>
      <div className="flex gap-2">
        <Button type="submit">保存</Button>
        <Button type="button" variant="outline" onClick={onTest}>测试终端模板</Button>
      </div>
    </form>
  );
}
