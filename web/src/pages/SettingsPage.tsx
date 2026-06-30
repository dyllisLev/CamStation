import { Bell, Clock, Database, HardDrive, KeyRound, SlidersHorizontal, Video } from "lucide-react";
import { FeatureMatrix } from "../components/FeatureMatrix";

export function SettingsPage() {
  return (
    <FeatureMatrix
      title="NVR Settings"
      items={[
        { icon: Video, title: "Segment Length", status: "unknown", detail: "Recording segment policy will move from legacy settings into Go." },
        { icon: Clock, title: "Retention Days", status: "unknown", detail: "Retention policy should drive cleanup, storage warnings, and UI timeline." },
        { icon: HardDrive, title: "Max Storage", status: "unknown", detail: "Storage cap handling must be explicit before production recording." },
        { icon: Bell, title: "Alert Rules", status: "unknown", detail: "Webhook, cooldown, acknowledge, and snooze settings belong here." },
        { icon: KeyRound, title: "Secrets", status: "unknown", detail: "RTSP and webhook secrets need export-safe handling." },
        { icon: Database, title: "Import/Export", status: "unknown", detail: "Camera import comes first; recording metadata import comes later." },
        { icon: SlidersHorizontal, title: "Profiles", status: "unknown", detail: "Camera profile defaults should cover transport and keepalive policies." },
      ]}
    />
  );
}

