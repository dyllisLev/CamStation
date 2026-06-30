import { cn } from "../lib/utils";

const styles: Record<string, string> = {
  ok: "bg-emerald-400 shadow-emerald-400/40",
  running: "bg-emerald-400 shadow-emerald-400/40",
  streaming: "bg-emerald-400 shadow-emerald-400/40",
  error: "bg-red-400 shadow-red-400/40",
  offline: "bg-red-400 shadow-red-400/40",
  degraded: "bg-amber-400 shadow-amber-400/40",
  unknown: "bg-slate-500 shadow-slate-500/40",
};

export function StatusDot({ status }: { status: string }) {
  return (
    <span
      className={cn(
        "inline-block size-2 rounded-full shadow-[0_0_12px]",
        styles[status] ?? styles.unknown,
      )}
    />
  );
}

