export type StartupStatus = {
  readonly configured: boolean;
  readonly autoStart: boolean;
  readonly leaseAvailable: boolean;
  readonly connection?: string;
};

export function startupAction(status: StartupStatus, autoStartLaunch: boolean): "show_setup" | "quit" | "acquire_lease" {
  if (!status.configured) return "show_setup";
  if (status.connection && status.connection !== "online") return "show_setup";
  if ((autoStartLaunch && !status.autoStart) || !status.leaseAvailable) return "quit";
  return "acquire_lease";
}

export function disconnectAction({ explicitShutdown }: { readonly explicitShutdown: boolean; readonly retryCount: number }): "quit" | "show_service_error_and_reconnect" {
  return explicitShutdown ? "quit" : "show_service_error_and_reconnect";
}

export function reconnectDelaySeconds(retryCount: number): 1 | 2 | 5 | 10 | 30 {
  return [1, 2, 5, 10, 30][Math.min(Math.max(retryCount, 0), 4)] as 1 | 2 | 5 | 10 | 30;
}

export function setupLoadAction(setupVisible: boolean): "load" | "preserve" {
  return setupVisible ? "preserve" : "load";
}
