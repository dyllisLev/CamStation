import type { CameraProfileTemplateChannel, CameraProfileTemplateStream, StreamCandidate } from "../../app/api";
import { ProfileLibraryValidationError } from "./profileLibraryErrors";

type DraftChannel = {
  readonly index: number;
  readonly name: string;
  readonly streams: CameraProfileTemplateStream[];
};

export function channelsFromMappingText(mappingText: string): CameraProfileTemplateChannel[] {
  const lines = mappingText
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0 && !line.startsWith("#"));
  if (lines.length === 0) {
    throw new ProfileLibraryValidationError("채널 매핑을 한 줄 이상 입력하세요.");
  }
  const channels: DraftChannel[] = [];
  lines.forEach((line, index) => addMappingLine(channels, line, index + 1));
  return channels
    .sort((left, right) => left.index - right.index)
    .map((channel) => ({ index: channel.index, name: channel.name, streams: channel.streams }));
}

export function streamLine(channel: CameraProfileTemplateChannel, stream: CameraProfileTemplateStream): string {
  return [
    channel.index,
    channel.name,
    stream.role,
    stream.label,
    stream.source,
    stream.path,
    stream.profileToken ?? "",
    stream.codec ?? "",
    stream.width ?? "",
    stream.height ?? "",
    stream.fps ?? "",
    stream.bitrateKbps ?? "",
  ].join("|");
}

export function mappingLine(channelIndex: number, channelName: string, candidate: StreamCandidate | undefined): string {
  if (!candidate) return "";
  return [
    channelIndex,
    channelName || `channel ${channelIndex}`,
    candidate.roleHint,
    candidate.label,
    candidate.source || "scan",
    pathFromCandidate(candidate),
    candidate.profileToken ?? "",
    candidate.codec ?? "",
    candidate.width ?? "",
    candidate.height ?? "",
    candidate.fps ?? "",
    candidate.bitrateKbps ?? "",
  ].join("|");
}

function addMappingLine(channels: DraftChannel[], line: string, lineNumber: number) {
  const cells = line.split("|").map((cell) => cell.trim());
  const channelIndex = parseInteger(cells[0], `매핑 ${lineNumber}행의 채널`);
  const role = requiredCell(cells[2], `매핑 ${lineNumber}행의 역할`);
  const label = requiredCell(cells[3], `매핑 ${lineNumber}행의 라벨`);
  const source = requiredCell(cells[4], `매핑 ${lineNumber}행의 소스`);
  const path = requiredCell(cells[5], `매핑 ${lineNumber}행의 경로`);
  if (path.includes("://") || path.includes("@")) {
    throw new ProfileLibraryValidationError(`매핑 ${lineNumber}행 경로에는 URL 또는 계정 정보를 넣을 수 없습니다.`);
  }
  const stream: CameraProfileTemplateStream = {
    role,
    label,
    source,
    path,
    profileToken: cells[6] ?? "",
    codec: cells[7] ?? "",
    width: parseOptionalInteger(cells[8], `매핑 ${lineNumber}행의 너비`),
    height: parseOptionalInteger(cells[9], `매핑 ${lineNumber}행의 높이`),
    fps: parseOptionalNumber(cells[10], `매핑 ${lineNumber}행의 FPS`),
    bitrateKbps: parseOptionalInteger(cells[11], `매핑 ${lineNumber}행의 비트레이트`),
  };
  const channelName = cells[1] || `channel ${channelIndex}`;
  const channel = channels.find((item) => item.index === channelIndex);
  if (channel) {
    channel.streams.push(stream);
    return;
  }
  channels.push({ index: channelIndex, name: channelName, streams: [stream] });
}

function pathFromCandidate(candidate: StreamCandidate): string {
  const values = [candidate.redactedUrl, candidate.source].filter((value): value is string => Boolean(value));
  for (const value of values) {
    if (value.startsWith("/")) return value;
    if (!value.includes("://")) continue;
    if (!URL.canParse(value)) continue;
    const parsed = new URL(value);
    return `${parsed.pathname}${parsed.search}`;
  }
  return candidate.profileToken ? `/profiles/${candidate.profileToken}` : `/${candidate.roleHint}`;
}

function requiredCell(value: string | undefined, label: string): string {
  const trimmed = value?.trim() ?? "";
  if (!trimmed) {
    throw new ProfileLibraryValidationError(`${label} 값을 입력하세요.`);
  }
  return trimmed;
}

function parseInteger(value: string | undefined, label: string): number {
  const parsed = Number.parseInt(requiredCell(value, label), 10);
  if (!Number.isFinite(parsed) || parsed < 0) {
    throw new ProfileLibraryValidationError(`${label} 값은 0 이상의 숫자여야 합니다.`);
  }
  return parsed;
}

function parseOptionalInteger(value: string | undefined, label: string): number | undefined {
  if (!value) return undefined;
  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed) || parsed < 0) {
    throw new ProfileLibraryValidationError(`${label} 값은 0 이상의 숫자여야 합니다.`);
  }
  return parsed;
}

function parseOptionalNumber(value: string | undefined, label: string): number | undefined {
  if (!value) return undefined;
  const parsed = Number.parseFloat(value);
  if (!Number.isFinite(parsed) || parsed < 0) {
    throw new ProfileLibraryValidationError(`${label} 값은 0 이상의 숫자여야 합니다.`);
  }
  return parsed;
}
