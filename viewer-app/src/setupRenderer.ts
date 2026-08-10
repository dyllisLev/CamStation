import type { ViewerStatus } from "./managementPipe.js";
import { setupErrorMessage, setupHydration } from "./setupModel.js";

type Result = { readonly ok?: boolean; readonly errorCode?: string };
type SetupStatus = Pick<ViewerStatus, "config" | "autoStart" | "connection">;
type Bridge = {
  getSetupState(): Promise<SetupStatus | null>;
  saveConfiguration(draft: unknown): Promise<Result>;
  retryConnection(): Promise<SetupStatus | null>;
};

const bridge = (globalThis as unknown as { camstationViewer: Bridge }).camstationViewer;
const server = document.querySelector<HTMLInputElement>("#server-url")!;
const displayName = document.querySelector<HTMLInputElement>("#display-name")!;
const autoStart = document.querySelector<HTMLInputElement>("#auto-start")!;
const message = document.querySelector<HTMLElement>("#message")!;
const setupInputs = [server, displayName, autoStart];
let dirty = false;

for (const input of setupInputs) {
  input.addEventListener(input === autoStart ? "change" : "input", () => {
    dirty = true;
  });
}

function applyStatus(status: SetupStatus | null): void {
  const editing = setupInputs.some((input) => document.activeElement === input);
  const hydration = setupHydration(status, { dirty, editing });
  if (hydration.draft) {
    server.value = hydration.draft.serverUrl;
    displayName.value = hydration.draft.displayName;
    autoStart.checked = hydration.draft.autoStart;
  }
  message.textContent = hydration.message ?? "서버 연결 정보를 입력해 주세요.";
  if (hydration.focusServer) server.focus({ preventScroll: true });
}

async function refresh(): Promise<void> {
  applyStatus(await bridge.getSetupState());
}

document.querySelector<HTMLFormElement>("#connection-form")!.addEventListener("submit", (event) => {
  event.preventDefault();
  void bridge.saveConfiguration({
    serverUrl: server.value,
    displayName: displayName.value,
    autoStart: autoStart.checked,
  }).then((result) => {
    if (!result?.ok) message.textContent = setupErrorMessage(result?.errorCode ?? "");
  }).catch(() => {
    message.textContent = setupErrorMessage("service_unavailable");
  });
});

document.querySelector<HTMLButtonElement>("#retry")!.addEventListener("click", () => {
  void bridge.retryConnection().then(applyStatus).catch(() => {
    message.textContent = setupErrorMessage("service_unavailable");
  });
});

void refresh();
