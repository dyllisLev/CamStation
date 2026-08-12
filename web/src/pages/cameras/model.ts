import type { CameraScanRequest, CameraStreamSelection, DeviceProfile, StreamCandidate } from "../../app/api";

export type CameraFormState = {
  name: string;
  streamName: string;
  host: string;
  username: string;
  password: string;
  rtspPort: string;
  httpPort: string;
  onvifPort: string;
  adapter: string;
};

export type RoleSelection = {
  channelIndex: number;
  recordingProfileToken: string;
  liveProfileToken: string;
};

export type PreviewTarget = {
  streamName: string;
  label: string;
};

export const initialForm: CameraFormState = {
  name: "염소장",
  streamName: "goat-yard",
  host: "192.168.0.55",
  username: "",
  password: "",
  rtspPort: "10554",
  httpPort: "10080",
  onvifPort: "10080",
  adapter: "auto",
};

export function toScanRequest(form: CameraFormState): CameraScanRequest {
  const username = form.username.trim();
  const password = form.password;
  return {
    name: form.name.trim() || undefined,
    host: form.host.trim(),
    username: username && password ? username : undefined,
    password: username && password ? password : undefined,
    rtspPort: parsePort(form.rtspPort),
    httpPort: parsePort(form.httpPort),
    onvifPort: parsePort(form.onvifPort),
    adapter: form.adapter,
  };
}

export function defaultRoleSelection(profile: DeviceProfile): RoleSelection {
  const channel = profile.channels[0];
  const candidates = channel?.candidates ?? [];
  const recording = preferredCandidate(candidates, "recording");
  const live = preferredCandidate(candidates, "live") ?? recording;
  return {
    channelIndex: channel?.index ?? 0,
    recordingProfileToken: recording?.profileToken ?? "",
    liveProfileToken: live?.profileToken ?? "",
  };
}

export function candidatesForChannel(profile: DeviceProfile, channelIndex: number): StreamCandidate[] {
  return profile.channels.find((channel) => channel.index === channelIndex)?.candidates ?? profile.channels[0]?.candidates ?? [];
}

export function selectedCandidate(profile: DeviceProfile, channelIndex: number, profileToken: string): StreamCandidate | undefined {
  return candidatesForChannel(profile, channelIndex).find((candidate) => candidate.profileToken === profileToken);
}

export function streamSelections(selection: RoleSelection): CameraStreamSelection[] {
  return [
    { role: "recording", profileToken: selection.recordingProfileToken },
    { role: "live", profileToken: selection.liveProfileToken },
  ].filter((item) => item.profileToken);
}

export function selectionReady(selection: RoleSelection): boolean {
  return Boolean(selection.recordingProfileToken && selection.liveProfileToken);
}

export function candidateLabel(candidate: StreamCandidate): string {
  const details = [formatSize(candidate.width, candidate.height), candidate.codec, candidate.fps ? `${candidate.fps}fps` : ""]
    .filter(Boolean)
    .join(" · ");
  return details ? `${candidate.label} (${details})` : candidate.label;
}

export function roleLabel(role: string): string {
  if (role === "recording") return "녹화";
  if (role === "live") return "라이브";
  if (role === "snapshot") return "스냅샷";
  return role;
}

export function formatSize(width?: number, height?: number): string {
  if (!width || !height) return "-";
  return `${width}x${height}`;
}

function preferredCandidate(candidates: StreamCandidate[], role: string): StreamCandidate | undefined {
  return candidates.find((candidate) => candidate.roleHint === role) ?? candidates[0];
}

function parsePort(value: string): number | undefined {
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
}
