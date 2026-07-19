import type { ConfigDraft } from "./managementPipe.js";

export type SetupState = {
  readonly draft: ConfigDraft;
  readonly activeConfig?: { readonly serverUrl: string; readonly displayName: string };
  readonly errorCode?: string;
};

export function nextSetupState(previous: SetupState, draft: ConfigDraft, errorCode?: string): SetupState {
  return errorCode ? { draft, activeConfig: previous.activeConfig, errorCode } : { draft };
}

export function setupErrorMessage(errorCode: string): string {
  switch (errorCode) {
  case "invalid_input": return "입력값을 확인해 주세요.";
  case "server_unreachable": return "서버에 연결할 수 없습니다.";
  case "api_incompatible": return "서버 버전이 호환되지 않습니다.";
  case "registration_rejected": return "Viewer 등록이 거부되었습니다.";
  default: return "설정을 저장할 수 없습니다.";
  }
}
