import type {
  Camera,
  CameraSourceKey,
  StreamCandidate,
  StreamOutputMutationResponse,
  StreamOutputSettings,
  StreamOutputSettingsTuple,
  UpdateStreamOutputsRequest,
} from "../../app/cameraTypes";

export const CAMERA_POLICY_INVALIDATION_KEYS = [
  ["cameras"],
  ["stream-status"],
  ["streams", "status"],
  ["recorder-status"],
  ["events"],
] as const;

export type StreamPolicyDraft = {
  cameraKey: string;
  baseRevision: number;
  serverRevision: number;
  outputs: StreamOutputSettingsTuple;
  dirty: boolean;
};

export type PolicyMutationNotice = {
  state: "applied" | "pending" | "conflict";
  message: string;
};

export function recommendedStreamOutputs(hasLiveSource: boolean): StreamOutputSettingsTuple {
  return [
    output("recording", "recording", "copy", null, null, null, "source", "on_demand"),
    output("live", hasLiveSource ? "live" : "recording", "auto", null, null, null, "none", "on_demand"),
    output("focus", "recording", "auto", 1920, 1080, null, "none", "on_demand"),
  ];
}

export function draftFromCamera(camera: Camera): StreamPolicyDraft {
  const byPurpose = new Map(camera.streamOutputs.map((item) => [item.purpose, item.desired]));
  const fallback = recommendedStreamOutputs(camera.streams?.some((item) => item.sourceKey === "live") ?? false);
  return {
    cameraKey: camera.streamName,
    baseRevision: camera.streamApplyState.desiredRevision,
    serverRevision: camera.streamApplyState.desiredRevision,
    outputs: fallback.map((item) => ({ ...(byPurpose.get(item.purpose) ?? item) })) as StreamOutputSettingsTuple,
    dirty: false,
  };
}

export function reconcilePolicyDraft(current: StreamPolicyDraft, camera: Camera): StreamPolicyDraft {
  if (current.cameraKey !== camera.streamName) return draftFromCamera(camera);
  return { ...current, serverRevision: camera.streamApplyState.desiredRevision };
}

export function reloadedPolicyDraft(cameras: readonly Camera[], cameraKey: string): StreamPolicyDraft | null {
  const camera = cameras.find((item) => item.streamName === cameraKey);
  return camera ? draftFromCamera(camera) : null;
}

export function hasDistinctLiveSource(
  candidates: readonly Pick<StreamCandidate, "profileToken" | "redactedUrl" | "source">[],
  recordingProfileToken: string,
  liveProfileToken: string,
): boolean {
  if (!recordingProfileToken || !liveProfileToken || recordingProfileToken === liveProfileToken) return false;
  const recording = candidates.find((item) => item.profileToken === recordingProfileToken);
  const live = candidates.find((item) => item.profileToken === liveProfileToken);
  if (!recording || !live) return false;
  const recordingIdentity = recording.redactedUrl || recording.source;
  const liveIdentity = live.redactedUrl || live.source;
  return Boolean(recordingIdentity && liveIdentity && recordingIdentity !== liveIdentity);
}

export function normalizeUnavailableSources(
  outputs: StreamOutputSettingsTuple,
  availableSourceKeys: readonly CameraSourceKey[],
): StreamOutputSettingsTuple {
  return outputs.map((output) => availableSourceKeys.includes(output.sourceKey)
    ? { ...output }
    : { ...output, sourceKey: "recording" }) as StreamOutputSettingsTuple;
}

export function streamOutputUpdateRequest(draft: StreamPolicyDraft): UpdateStreamOutputsRequest {
  return { expectedDesiredRevision: draft.baseRevision, outputs: draft.outputs };
}

export function validateStreamOutputs(outputs: readonly StreamOutputSettings[], availableSourceKeys: readonly CameraSourceKey[] = ["recording", "live"]): string | null {
  if (outputs.length !== 3 || new Set(outputs.map((item) => item.purpose)).size !== 3) {
    return "녹화·라이브·집중보기 정책이 모두 필요합니다.";
  }
  for (const item of outputs) {
    if (!availableSourceKeys.includes(item.sourceKey)) return `${purposeLabel(item.purpose)}에서 사용할 수 없는 원본 입력입니다.`;
    const hasWidth = item.maxWidth !== null;
    const hasHeight = item.maxHeight !== null;
    if (hasWidth !== hasHeight) return `${purposeLabel(item.purpose)} 최대 폭과 높이를 함께 입력하세요.`;
    if (hasWidth && hasHeight && ((item.maxWidth ?? 0) < 2 || (item.maxHeight ?? 0) < 2)) {
      return `${purposeLabel(item.purpose)} 최대 해상도가 올바르지 않습니다.`;
    }
    if (hasWidth && hasHeight && ((item.maxWidth ?? 0) % 2 !== 0 || (item.maxHeight ?? 0) % 2 !== 0)) {
      return `${purposeLabel(item.purpose)} 최대 해상도는 짝수여야 합니다.`;
    }
    if (item.maxFPS !== null && (!Number.isInteger(item.maxFPS) || item.maxFPS < 1 || item.maxFPS > 60)) {
      return `${purposeLabel(item.purpose)} 최대 FPS는 1–60 정수여야 합니다.`;
    }
    if (item.videoMode === "copy" && (hasWidth || item.maxFPS !== null)) {
      return `${purposeLabel(item.purpose)} 원본 복사에서는 해상도나 FPS를 제한할 수 없습니다.`;
    }
  }
  return null;
}

export function policyMutationNotice(response?: StreamOutputMutationResponse, status?: number): PolicyMutationNotice {
  if (status === 409) {
    return { state: "conflict", message: "서버 설정이 변경되었습니다. 서버값을 다시 불러오세요." };
  }
  if (response && !response.applied) {
    return { state: "pending", message: response.warning || "저장되었지만 런타임 적용을 기다리고 있습니다." };
  }
  return { state: "applied", message: "저장 및 적용이 완료되었습니다." };
}

export function purposeLabel(purpose: StreamOutputSettings["purpose"]): string {
  if (purpose === "recording") return "녹화";
  if (purpose === "live") return "라이브";
  return "집중보기";
}

function output(
  purpose: StreamOutputSettings["purpose"],
  sourceKey: StreamOutputSettings["sourceKey"],
  videoMode: StreamOutputSettings["videoMode"],
  maxWidth: number | null,
  maxHeight: number | null,
  maxFPS: number | null,
  audioMode: StreamOutputSettings["audioMode"],
  activation: StreamOutputSettings["activation"],
): StreamOutputSettings {
  return { purpose, sourceKey, videoMode, maxWidth, maxHeight, maxFPS, audioMode, activation };
}
