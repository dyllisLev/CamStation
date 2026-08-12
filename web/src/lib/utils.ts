import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatDate(value?: string) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(date);
}

export function formatDurationNanos(value?: number) {
  if (!value) return "-";
  return `${(value / 1_000_000_000).toFixed(2)}s`;
}

export function liveUrl(streamName: string, mode: "mse" | "webrtc" = "mse") {
  return `/player/stream.html?src=${encodeURIComponent(streamName)}&mode=${mode}`;
}
