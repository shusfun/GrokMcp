import type { Diagnose } from "../api/client";

function Flag({ ok, yes, no }: { ok: boolean; yes: string; no: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={`inline-block h-1.5 w-1.5 rounded-full ${ok ? "bg-[var(--green)]" : "bg-[var(--fail)]"}`} />
      {ok ? yes : no}
    </span>
  );
}

const ROWS: Array<[string, (d: Diagnose) => string | ReturnType<typeof Flag>]> = [
  ["Grok 路径", (d) => d.grok_path || "未找到"],
  ["版本", (d) => d.grok_version || "—"],
  ["兼容", (d) => <Flag ok={d.compatible} yes="是" no="否" />],
  ["已登录", (d) => <Flag ok={d.logged_in} yes="是" no="否" />],
  ["Leader", (d) => <Flag ok={d.leader_running} yes="运行中" no="未运行" />],
  ["Leader socket", (d) => d.leader_socket || "—"],
  ["附着路径", (d) => d.attach_mode || "—"],
];

export function DiagnoseList({ data }: { data: Diagnose }) {
  return (
    <dl className="divide-y divide-[var(--line)] border-t border-[var(--line)]">
      {ROWS.map(([label, value]) => (
        <div key={label} className="grid grid-cols-3 gap-4 py-2.5 text-sm">
          <dt className="text-[var(--muted)]">{label}</dt>
          <dd className="col-span-2 break-all">{value(data)}</dd>
        </div>
      ))}
      {data.error ? (
        <div className="py-2.5 text-sm text-[var(--fail)]">{data.error}</div>
      ) : null}
    </dl>
  );
}
