import { Download, HeartPulse, Monitor, Radio, RotateCw, Send } from "lucide-react";
import { FeatureMatrix } from "../components/FeatureMatrix";

export function ViewersPage() {
  return (
    <FeatureMatrix
      title="Viewer App Fleet"
      items={[
        { icon: HeartPulse, title: "Heartbeat", status: "unknown", detail: "Viewer heartbeat state from the legacy app needs a Go model." },
        { icon: Monitor, title: "Client Status", status: "unknown", detail: "Healthy/degraded viewer counts belong here and in Control Room." },
        { icon: Send, title: "Commands", status: "unknown", detail: "Pending command delivery will replace ad hoc viewer controls." },
        { icon: Download, title: "Installer", status: "unknown", detail: "Windows viewer download and version state will be exposed here." },
        { icon: RotateCw, title: "Updater", status: "unknown", detail: "Viewer app update flow is part of the system manager." },
        { icon: Radio, title: "Viewer Mode", status: "unknown", detail: "A viewer-only surface should be separated from admin APIs." },
      ]}
    />
  );
}

