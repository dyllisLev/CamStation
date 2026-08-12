import { withAppBase } from "../../app/basePath";

export function formatBytes(bytes: number | null | undefined) {
  if (!Number.isFinite(bytes) || !bytes || bytes <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(value >= 10 || unit === 0 ? 0 : 1)} ${units[unit]}`;
}

export function bytesToGB(bytes: number | undefined) {
  return (bytes ?? 0) / 1024 / 1024 / 1024;
}

export function formatSegmentTime(value: number | null | undefined) {
  if (!value) {
    return "-";
  }
  return new Intl.DateTimeFormat("ko-KR", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(new Date(value * 1000));
}

export function formatDuration(start: number, end: number | null) {
  if (!end || end <= start) {
    return "진행 중";
  }
  const seconds = Math.round(end - start);
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  return `${minutes}분 ${rest}초`;
}

export function toSegmentTimeFilter(value: string) {
  if (!value) {
    return undefined;
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return undefined;
  }
  return date.toISOString();
}

export function safeSegmentUrl(url: string | undefined, id: number, action: "play" | "download") {
  const expected = `/api/recordings/segments/${id}/${action}`;
  if (url !== expected) {
    return undefined;
  }
  return withAppBase(url);
}

export function statusLabel(status: string) {
  switch (status) {
    case "ready":
      return "완료";
    case "recording":
      return "녹화 중";
    case "finalizing":
      return "마무리";
    case "deleted":
      return "삭제됨";
    case "failed":
      return "실패";
    default:
      return status || "알 수 없음";
  }
}
