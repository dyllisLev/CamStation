import { RotateCcw } from "lucide-react";
import { useRestartStreams, useStreamStatus } from "../app/queries";
import { Button } from "../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../components/ui/panel";

export function SystemPage() {
  const streams = useStreamStatus();
  const restart = useRestartStreams();

  return (
    <div className="grid gap-4 xl:grid-cols-2">
      <Panel>
        <PanelHeader>
          <h2 className="text-sm font-semibold">Stream Manager</h2>
        </PanelHeader>
        <PanelBody className="space-y-4">
          <div className="grid gap-2 text-sm">
            <div className="flex justify-between gap-4">
              <span className="text-slate-500">Installed</span>
              <span>{streams.data?.installed ? "yes" : "no"}</span>
            </div>
            <div className="flex justify-between gap-4">
              <span className="text-slate-500">Running</span>
              <span>{streams.data?.running ? "yes" : "no"}</span>
            </div>
            <div className="flex justify-between gap-4">
              <span className="text-slate-500">API</span>
              <span>{streams.data?.apiUrl ?? "-"}</span>
            </div>
          </div>
          <Button
            type="button"
            variant="secondary"
            onClick={() => restart.mutate()}
            disabled={restart.isPending}
          >
            <RotateCcw size={16} />
            Restart go2rtc
          </Button>
        </PanelBody>
      </Panel>
    </div>
  );
}

