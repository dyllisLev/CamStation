import { Download, FileArchive, PackageCheck, Power, RotateCcw, ServerCog, Wrench } from "lucide-react";
import { useRestartStreams, useStreamStatus } from "../app/queries";
import { FeatureMatrix } from "../components/FeatureMatrix";
import { Button } from "../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../components/ui/panel";

export function SystemPage() {
  const streams = useStreamStatus();
  const restart = useRestartStreams();

  return (
    <div className="space-y-4">
      <FeatureMatrix
        title="System Operations"
        items={[
          { icon: ServerCog, title: "Service State", status: "running", detail: "camstationd is serving API and embedded React console." },
          { icon: PackageCheck, title: "Version", status: "unknown", detail: "Release/version checks from the legacy updater need a Go API." },
          { icon: Download, title: "Update", status: "unknown", detail: "Update orchestration should run from one managed workflow." },
          { icon: FileArchive, title: "Diagnostics", status: "unknown", detail: "Diagnostic bundles should include config, logs, and process state." },
          { icon: Power, title: "Restart", status: "unknown", detail: "Restart controls need guardrails before production use." },
          { icon: Wrench, title: "Maintenance", status: "unknown", detail: "Cleanup, DB vacuum, and health checks belong in this panel." },
        ]}
      />
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
