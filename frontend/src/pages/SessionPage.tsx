import { useParams } from "react-router";
import { SessionDetail } from "../components/SessionDetail";

export function SessionPage() {
  const { jobId = "" } = useParams();
  return (
    <div className="h-full min-h-0 bg-[var(--surface)]">
      {jobId ? <SessionDetail jobId={jobId} /> : null}
    </div>
  );
}
