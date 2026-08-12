import type { RecordingSegment } from "../../app/api";

export function backupStateLabel(segment: RecordingSegment) {
  if (segment.backupState === "backed_up") {
    return "백업됨";
  }
  if (segment.status === "deleted") {
    return "원본 없음";
  }
  if (segment.status !== "ready") {
    return "대상 아님";
  }
  return "백업 대기";
}
