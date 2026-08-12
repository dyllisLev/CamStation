import { BackupConfigPanel } from "./backup/BackupConfigPanel";
import { BackupHistoryPanel } from "./backup/BackupHistoryPanel";
import { BackupJobPanel } from "./backup/BackupJobPanel";

export function BackupPage() {
  return (
    <div className="space-y-4">
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
        <BackupJobPanel />
        <BackupConfigPanel />
      </div>
      <BackupHistoryPanel />
    </div>
  );
}
