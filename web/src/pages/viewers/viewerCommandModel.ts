import type { Viewer, ViewerCommandState } from "../../app/api";

export type OperatorViewerCommand =
  | "ping"
  | "reload_live"
  | "resubscribe_stream"
  | "restart_viewer"
  | "restart_service";

export type ViewerCommandAction = {
  readonly type: OperatorViewerCommand;
  readonly label: string;
  readonly description: string;
  readonly disruptive: boolean;
  readonly requiresStream: boolean;
};

export const VIEWER_COMMAND_ACTIONS: readonly ViewerCommandAction[] = [
  { type: "ping", label: "제어 연결 확인", description: "Viewer 관리 서비스가 서버 명령을 실제로 처리하는지 확인합니다.", disruptive: false, requiresStream: false },
  { type: "reload_live", label: "라이브 화면 새로고침", description: "현재 Viewer의 승인된 라이브 화면을 다시 불러옵니다.", disruptive: false, requiresStream: false },
  { type: "resubscribe_stream", label: "카메라 영상 다시 연결", description: "선택한 카메라의 Viewer 재생 연결만 새로 만듭니다.", disruptive: false, requiresStream: true },
  { type: "restart_viewer", label: "Viewer 앱 시작 또는 다시 시작", description: "Viewer 앱을 다시 열고 새 renderer가 준비될 때까지 확인합니다.", disruptive: true, requiresStream: false },
  { type: "restart_service", label: "Viewer 관리 서비스 다시 시작", description: "Windows 관리 서비스를 다시 시작하고 서버 제어 연결 복구를 확인합니다.", disruptive: true, requiresStream: false },
] as const;

const terminalStates = new Set<ViewerCommandState>(["succeeded", "failed", "rejected", "expired", "cancelled", "deleted"]);

export function viewerCommandAction(type: string): ViewerCommandAction {
  return VIEWER_COMMAND_ACTIONS.find((action) => action.type === type) ?? VIEWER_COMMAND_ACTIONS[0];
}

export function viewerCommandIsActive(state: ViewerCommandState): boolean {
  return !terminalStates.has(state);
}

export function viewerCommandUnavailableReason(
  viewer: Viewer | undefined,
  action: ViewerCommandAction,
  streamName: string,
  configuredStreamNames?: ReadonlySet<string>,
): string {
  if (!viewer) return "대상 Viewer를 선택하세요.";
  if (viewer.status === "offline" || viewer.status === "stale") return "Viewer 관리 서비스가 오프라인입니다.";
  if (viewer.control?.state !== "online") return "Viewer 제어 채널이 온라인 상태가 아닙니다.";
  if ((action.type === "reload_live" || action.type === "resubscribe_stream") && viewer.viewer?.state !== "running") {
    return "Viewer 앱이 실행 중이 아닙니다.";
  }
  if ((action.type === "reload_live" || action.type === "resubscribe_stream") && viewer.renderer?.state !== "ready") {
    return "Viewer 화면이 준비되지 않았습니다.";
  }
  if (action.requiresStream && !streamName) return "다시 연결할 카메라를 선택하세요.";
  const streamIsRegistered = configuredStreamNames !== undefined
    ? configuredStreamNames.has(streamName)
    : viewer.streams?.some((stream) => stream.streamName === streamName);
  if (action.requiresStream && !streamIsRegistered) {
    return "선택한 카메라가 이 Viewer에 등록되어 있지 않습니다.";
  }
  return "";
}

export function viewerCommandTypeLabel(type: string): string {
  if (type === "restart_agent") return viewerCommandAction("restart_service").label;
  return VIEWER_COMMAND_ACTIONS.find((action) => action.type === type)?.label ?? type;
}

export function viewerCommandErrorLabel(code?: string): string {
  switch (code) {
    case "":
    case undefined:
      return "";
    case "viewer_unavailable":
      return "Viewer 앱에 연결할 수 없습니다.";
    case "viewer_start_failed":
      return "로그인된 Windows 세션에서 Viewer 앱을 시작하지 못했습니다.";
    case "interactive_session_unavailable":
      return "로그인된 Windows 사용자 세션이 없습니다.";
    case "viewer_command_timeout":
      return "Viewer 명령 응답 시간이 초과되었습니다.";
    case "renderer_failed":
      return "Viewer 화면에서 선택한 카메라 작업을 처리하지 못했습니다.";
    case "viewer_command_failed":
    case "viewer_relaunch_failed":
      return "Viewer 앱이 요청한 작업을 완료하지 못했습니다.";
    case "viewer_restart_timeout":
      return "Viewer 재시작 확인 시간이 초과되었습니다.";
    case "service_restart_unavailable":
      return "이 환경에서는 Viewer 관리 서비스 재시작을 지원하지 않습니다.";
    case "service_restart_failed":
      return "Viewer 관리 서비스를 다시 시작하지 못했습니다.";
    case "command_expired":
      return "명령 유효 시간이 지났습니다.";
    case "command_interrupted":
      return "이전 서비스 종료로 명령 실행이 중단되었습니다.";
    case "command_payload_changed":
      return "같은 명령 ID의 내용이 달라 안전하게 거부되었습니다.";
    case "unsupported_command":
      return "설치된 Viewer에서 지원하지 않는 명령입니다.";
    case "command_journal_unavailable":
      return "Viewer의 명령 실행 기록을 안전하게 저장하지 못했습니다.";
    case "execution_failed":
      return "Viewer 관리 서비스에서 명령 실행에 실패했습니다.";
    default:
      return "Viewer에서 명령을 완료하지 못했습니다.";
  }
}
