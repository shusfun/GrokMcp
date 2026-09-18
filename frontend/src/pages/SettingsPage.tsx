import { useEffect, useState } from "react";
import { useLocation } from "react-router";
import { getClient } from "../api";
import type { Diagnose, Settings, UpdateRelease } from "../api/client";
import { DiagnoseList } from "../components/DiagnoseList";
import { SettingsForm } from "../components/SettingsForm";
import { useTheme } from "../components/ThemeProvider";
import { Button } from "../components/ui/button";
import { Segmented } from "../components/ui/segmented";
import type { Appearance } from "../lib/theme";

export function SettingsPage() {
  const client = getClient();
  const diagnoseFocus = useLocation().pathname.startsWith("/diagnostics");
  const { appearance, setAppearance } = useTheme();
  const [value, setValue] = useState<Settings | null>(null);
  const [note, setNote] = useState("");
  const [diagnose, setDiagnose] = useState<Diagnose | null>(null);
  const [diagError, setDiagError] = useState("");
  const [version, setVersion] = useState("");
  const [updateNote, setUpdateNote] = useState("");
  const [checking, setChecking] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [installLog, setInstallLog] = useState("");

  useEffect(() => {
    void client.settings().then(setValue);
    void client.appVersion().then(setVersion).catch(() => setVersion(""));
    void client.diagnose().then(setDiagnose).catch((e: Error) => setDiagError(e.message));
  }, [client]);

  useEffect(() => {
    if (!diagnoseFocus) return;
    document.getElementById("diagnose")?.scrollIntoView({ block: "start" });
  }, [diagnoseFocus, diagnose]);

  return (
    <div className="h-full overflow-auto bg-[var(--surface)] p-6">
      <div className="mx-auto max-w-xl space-y-8">
        <section>
          <h1 className="text-base font-semibold">设置</h1>
          <p className="mt-1 text-sm text-[var(--muted)]">外观立即生效，其余项需保存。</p>
        </section>

        <section className="space-y-3">
          <h2 className="text-sm font-semibold">外观</h2>
          <Segmented
            aria-label="外观"
            value={appearance}
            options={[
              { value: "system", label: "跟随系统" },
              { value: "light", label: "浅色" },
              { value: "dark", label: "深色" },
            ]}
            onChange={(v) => setAppearance(v as Appearance)}
          />
        </section>

        <section className="space-y-3">
          <h2 className="text-sm font-semibold">Grok 与终端</h2>
          {value ? (
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
          ) : (
            <p className="text-sm text-[var(--muted)]">加载中…</p>
          )}
          {note ? <p className="text-sm text-[var(--accent)]">{note}</p> : null}
        </section>

        <section id="diagnose" className="space-y-3">
          <h2 className="text-sm font-semibold">运行状况</h2>
          {diagError ? <p className="text-sm text-[var(--fail)]">{diagError}</p> : null}
          {diagnose && !diagnose.grok_path ? (
            <div className="space-y-2 rounded-md border border-[var(--line)] bg-[var(--row)] p-3">
              <p className="text-sm">未找到 Grok。将安装到用户目录 <span className="font-mono text-xs">~/.grok/bin</span>，不需要管理员权限。</p>
              <Button
                size="sm"
                disabled={installing}
                onClick={() => {
                  setInstalling(true);
                  setInstallLog("");
                  void client.installGrok().then((res) => {
                    setInstallLog(res.log || (res.ok ? "安装完成" : "安装失败"));
                    return client.diagnose().then(setDiagnose);
                  }).catch((e: Error) => setInstallLog(e.message)).finally(() => setInstalling(false));
                }}
              >
                {installing ? "正在安装…" : "安装 Grok"}
              </Button>
              {installLog ? <pre className="max-h-40 overflow-auto whitespace-pre-wrap text-xs text-[var(--muted)]">{installLog}</pre> : null}
            </div>
          ) : null}
          {diagnose ? <DiagnoseList data={diagnose} /> : !diagError ? <p className="text-sm text-[var(--muted)]">诊断中…</p> : null}
        </section>

        <section className="space-y-3">
          <h2 className="text-sm font-semibold">关于</h2>
          <p className="text-sm text-[var(--muted)]">当前版本 {version || "dev"}</p>
          <Button
            size="sm"
            variant="outline"
            disabled={checking}
            onClick={() => {
              setChecking(true);
              setUpdateNote("");
              void client.checkUpdate().then((rel: UpdateRelease | null) => {
                setUpdateNote(rel?.version ? `发现新版本 ${rel.version}` : "已是最新版本");
              }).catch((e: Error) => setUpdateNote(e.message)).finally(() => setChecking(false));
            }}
          >
            检查更新
          </Button>
          {updateNote ? <p className="text-sm text-[var(--muted)]">{updateNote}</p> : null}
        </section>
      </div>
    </div>
  );
}
