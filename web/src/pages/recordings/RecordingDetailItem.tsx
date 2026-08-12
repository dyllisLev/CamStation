export function DetailItem({ label, value, mono }: { readonly label: string; readonly value: string; readonly mono?: boolean }) {
  return (
    <div className="grid grid-cols-[5rem_minmax(0,1fr)] gap-3 border-b border-slate-800 pb-2 last:border-0">
      <span className="text-xs font-medium text-slate-500">{label}</span>
      <span className={mono ? "break-all font-mono text-xs text-slate-300" : "break-words text-slate-200"}>{value}</span>
    </div>
  );
}
