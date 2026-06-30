import { cn } from "../../lib/utils";

const styles: Record<string, string> = {
  streaming: "border-emerald-500/40 bg-emerald-500/15 text-emerald-200",
  running: "border-emerald-500/40 bg-emerald-500/15 text-emerald-200",
  info: "border-sky-500/40 bg-sky-500/15 text-sky-200",
  offline: "border-red-500/40 bg-red-500/15 text-red-200",
  error: "border-red-500/40 bg-red-500/15 text-red-200",
  degraded: "border-amber-500/40 bg-amber-500/15 text-amber-200",
  warning: "border-amber-500/40 bg-amber-500/15 text-amber-200",
};

export function Badge({ value, className }: { value: string; className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex h-6 items-center rounded-full border px-2 text-xs font-medium capitalize",
        styles[value] ?? "border-slate-700 bg-slate-900 text-slate-300",
        className,
      )}
    >
      {value}
    </span>
  );
}

