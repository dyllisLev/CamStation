export function disconnectExitCode(explicitShutdown: boolean): 1 | null {
  return explicitShutdown ? null : 1;
}
