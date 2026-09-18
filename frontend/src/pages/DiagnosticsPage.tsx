import { useEffect, useState } from "react";
import { getClient } from "../api";
import type { Diagnose } from "../api/client";
import { DiagnoseList } from "../components/DiagnoseList";
import { ErrorState } from "../components/EmptyState";

export function DiagnosticsPage() {
  const [data, setData] = useState<Diagnose | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    void getClient().diagnose().then(setData).catch((e: Error) => setError(e.message));
  }, []);

  if (error) return <ErrorState message={error} />;
  if (!data) return <div className="p-4 text-sm text-[var(--muted)]">诊断中…</div>;
  return (
    <div className="p-4">
      <h1 className="mb-4 text-lg font-semibold">诊断</h1>
      <DiagnoseList data={data} />
    </div>
  );
}
