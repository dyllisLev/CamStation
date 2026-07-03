import { DiagnosticPanel } from "./system/DiagnosticPanel";
import { MaintenancePanel } from "./system/MaintenancePanel";
import { SystemStatusPanel } from "./system/SystemStatusPanel";

export function SystemPage() {
  return (
    <div className="space-y-4">
      <SystemStatusPanel />
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
        <DiagnosticPanel />
        <MaintenancePanel />
      </div>
    </div>
  );
}
