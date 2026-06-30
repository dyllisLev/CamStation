import { CalendarDays, Clapperboard, Download, Film, HardDrive, ListVideo, Scissors } from "lucide-react";
import { FeatureMatrix } from "../components/FeatureMatrix";

export function RecordingsPage() {
  return (
    <FeatureMatrix
      title="Recording Console"
      items={[
        { icon: Clapperboard, title: "Recorder Workers", status: "unknown", detail: "ffmpeg segment workers are the next backend milestone." },
        { icon: CalendarDays, title: "Daily Timeline", status: "unknown", detail: "Timeline data will come from recording metadata." },
        { icon: ListVideo, title: "Segment List", status: "unknown", detail: "Per-camera segment browsing replaces direct file inspection." },
        { icon: Film, title: "Playback", status: "unknown", detail: "Playback should support camera/date/segment selection." },
        { icon: Download, title: "Export", status: "unknown", detail: "Download and clip export belong in the recording surface." },
        { icon: Scissors, title: "Motion Events", status: "unknown", detail: "Motion markers should share the same timeline track." },
        { icon: HardDrive, title: "Storage Stats", status: "unknown", detail: "Recording storage and hourly growth need API support." },
      ]}
    />
  );
}
