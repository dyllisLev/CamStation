import { Archive, Cloud, FolderSync, HardDrive, RotateCcw, Trash2 } from "lucide-react";
import { FeatureMatrix } from "../components/FeatureMatrix";

export function BackupPage() {
  return (
    <FeatureMatrix
      title="Backup Pipeline"
      items={[
        { icon: Archive, title: "Backup Queue", status: "unknown", detail: "Queue state will be managed by camstationd instead of shell scripts." },
        { icon: Cloud, title: "Remote Target", status: "unknown", detail: "rclone target and credentials will live behind explicit settings." },
        { icon: FolderSync, title: "Sync Worker", status: "unknown", detail: "Worker supervision and retries are reserved for the backup manager." },
        { icon: RotateCcw, title: "Retry Policy", status: "unknown", detail: "Failed transfers need retry windows and incident integration." },
        { icon: Trash2, title: "Local Cleanup", status: "unknown", detail: "Retention and backup-safe deletion should share one policy." },
        { icon: HardDrive, title: "Storage Budget", status: "unknown", detail: "Storage pressure will be reflected in Control Room." },
      ]}
    />
  );
}

