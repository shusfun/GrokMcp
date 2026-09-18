import type { Settings } from "../api/client";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
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
        <Input className="mt-1" value={value.terminal_command_template} onChange={(e) => onChange({ ...value, terminal_command_template: e.target.value })} placeholder="{command}  {cwd}  {session_id}  {grok}" />
      </label>
      <label className="block text-sm text-[var(--muted)]">
        默认形态
        <Select
          className="mt-1 w-full"
          aria-label="默认形态"
          value={value.default_view_mode}
          options={[
            { value: "headless", label: "无头" },
            { value: "headed", label: "有头" },
          ]}
          onChange={(default_view_mode) => onChange({ ...value, default_view_mode })}
        />
      </label>
      <div className="flex gap-2">
        <Button type="submit">保存</Button>
        <Button type="button" variant="outline" onClick={onTest}>测试终端模板</Button>
      </div>
    </form>
  );
}
