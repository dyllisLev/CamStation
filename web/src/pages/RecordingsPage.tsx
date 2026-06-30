import { Clapperboard } from "lucide-react";
import { Panel, PanelBody } from "../components/ui/panel";

export function RecordingsPage() {
  return (
    <Panel>
      <PanelBody className="flex min-h-96 flex-col items-center justify-center gap-3 text-slate-500">
        <Clapperboard size={38} />
        <div className="text-sm">Recording timeline will appear after ffmpeg segment workers are added.</div>
      </PanelBody>
    </Panel>
  );
}

