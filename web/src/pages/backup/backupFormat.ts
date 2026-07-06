import type { Job } from "../../app/api";

export function formatDate(value?: string): string {
  if (!value) {
    return "-";
  }
  return new Date(value).toLocaleString("ko-KR", { hour12: false });
}

export function formatDuration(job: Job): string {
  if (!job.startedAt) {
    return "대기";
  }
  const end = job.completedAt ? new Date(job.completedAt).getTime() : Date.now();
  const start = new Date(job.startedAt).getTime();
  const seconds = Math.max(0, Math.round((end - start) / 1000));
  return `${seconds}s`;
}

export function jobBadgeState(state?: string): string {
	switch (state) {
	case "running":
		return "running";
	case "queued":
		return "queued";
	case "succeeded":
		return "succeeded";
	case "failed":
		return "failed";
	case "cancelled":
		return "cancelled";
	case "deleted":
		return "deleted";
	case undefined:
		return "offline";
	default:
      return state;
  }
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "요청 처리 중 오류가 발생했습니다.";
}
