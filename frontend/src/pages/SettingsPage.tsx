import { useEffect, useState } from "react";
import { getClient } from "../api";
import type { Settings } from "../api/client";
import { SettingsForm } from "../components/SettingsForm";

export function SettingsPage() {
  const client = getClient();
  const [value, setValue] = useState<Settings | null>(null);
  const [note, setNote] = useState("");

  useEffect(() => {
    void client.settings().then(setValue);
  }, [client]);

  if (!value) return <div className="p-4 text-sm text-[var(--muted)]">加载中…</div>;

  return (
    <div className="p-4">
      <h1 className="mb-4 text-lg font-semibold">设置</h1>
      <SettingsForm
        value={value}
        onChange={setValue}
        onSave={() => {
          void client.saveSettings(value).then(() => setNote("已保存"));
        }}
        onTest={() => {
          void client.testTerminal(value.terminal_command_template).then(
            () => setNote("模板测试已发出"),
            (e: Error) => setNote(e.message),
          );
        }}
      />
      {note ? <p className="mt-3 text-sm text-[var(--green)]">{note}</p> : null}
    </div>
  );
}
