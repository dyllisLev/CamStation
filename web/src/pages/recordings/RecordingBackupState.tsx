import type { RecordingSegment } from "../../app/api";
import { backupStateLabel } from "./recordingBackupStateLabel";

export function BackupStateBadge({ segment }: { readonly segment: RecordingSegment }) {
  const backedUp = segment.backupState === "backed_up";
  const unavailable = segment.status !== "ready";
  const className = backedUp
    ? "border-emerald-500/40 bg-emerald-500/15 text-emerald-200"
    : unavailable
      ? "border-slate-700 bg-slate-900 text-slate-400"
      : "border-amber-500/40 bg-amber-500/15 text-amber-200";
  return <span className={`inline-flex h-6 items-center rounded-full border px-2 text-xs font-medium ${className}`}>{backupStateLabel(segment)}</span>;
}
