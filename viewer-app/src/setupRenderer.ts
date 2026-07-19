type Result = { ok?: boolean; errorCode?: string };
type Bridge = { getSetupState(): Promise<any>; saveConfiguration(draft: unknown): Promise<Result>; retryConnection(): Promise<any> };
const bridge = (globalThis as unknown as { camstationViewer: Bridge }).camstationViewer;
const server = document.querySelector<HTMLInputElement>("#server-url")!;
const name = document.querySelector<HTMLInputElement>("#display-name")!;
const autoStart = document.querySelector<HTMLInputElement>("#auto-start")!;
const message = document.querySelector<HTMLElement>("#message")!;
const errorMessage: Record<string, string> = { invalid_input: "입력값을 확인해 주세요.", server_unreachable: "서버에 연결할 수 없습니다.", api_incompatible: "서버 버전이 호환되지 않습니다.", registration_rejected: "Viewer 등록이 거부되었습니다.", service_unavailable: "관리 서비스에 연결할 수 없습니다." };
async function refresh(): Promise<void> { const status = await bridge.getSetupState(); if (status?.config) { server.value = status.config.serverUrl; name.value = status.config.displayName; } autoStart.checked = status?.autoStart ?? true; if (status?.connection === "service_unavailable") message.textContent = errorMessage.service_unavailable; }
document.querySelector<HTMLFormElement>("#connection-form")!.addEventListener("submit", async (event) => { event.preventDefault(); const result = await bridge.saveConfiguration({ serverUrl: server.value, displayName: name.value, autoStart: autoStart.checked }); if (!result?.ok) message.textContent = errorMessage[result?.errorCode ?? ""] ?? "설정을 저장할 수 없습니다."; });
document.querySelector("#retry")!.addEventListener("click", () => void bridge.retryConnection().then(refresh));
void refresh();
