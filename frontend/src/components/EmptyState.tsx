export function EmptyState({ title, detail }: { title: string; detail?: string }) {
  return (
    <div className="px-4 py-20 text-center text-sm text-[var(--muted)]">
      <p className="font-medium text-[var(--ink)]">{title}</p>
      {detail ? <p className="mt-1">{detail}</p> : null}
    </div>
  );
}

export function ErrorState({ message }: { message: string }) {
  return <EmptyState title="无法加载" detail={message} />;
}
