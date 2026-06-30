import { Cable, EyeOff, RadioTower, RotateCcw, ShieldCheck, Wifi } from "lucide-react";
import { useCameras, useRestartStreams, useStreamStatus } from "../app/queries";
import { FeatureMatrix } from "../components/FeatureMatrix";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../components/ui/panel";

export function StreamsPage() {
  const cameras = useCameras();
  const streams = useStreamStatus();
  const restart = useRestartStreams();

  return (
    <div className="space-y-4">
      <FeatureMatrix
        title="Stream Core"
        items={[
          { icon: RadioTower, title: "go2rtc", status: streams.data?.running ? "running" : "offline", detail: streams.data?.apiUrl ?? "checking" },
          { icon: ShieldCheck, title: "API Exposure", status: "running", detail: "Raw go2rtc API is bound to localhost." },
          { icon: Wifi, title: "WebRTC", status: "running", detail: "Candidate list includes this server network address." },
          { icon: Cable, title: "MSE/fMP4", status: "running", detail: "Live player proxy is exposed through CamStation." },
          { icon: EyeOff, title: "Secret Redaction", status: "running", detail: "Camera URLs are redacted in CamStation API responses." },
        ]}
      />
      <Panel>
        <PanelHeader className="flex items-center justify-between gap-3">
          <h2 className="text-sm font-semibold">Generated Streams</h2>
          <Button type="button" variant="secondary" onClick={() => restart.mutate()} disabled={restart.isPending}>
            <RotateCcw size={16} />
            Restart
          </Button>
        </PanelHeader>
        <PanelBody>
          <div className="new-table-wrap">
            <table className="new-table">
              <thead>
                <tr>
                  <th className="px-3 py-2 font-medium">Stream</th>
                  <th className="px-3 py-2 font-medium">Camera</th>
                  <th className="px-3 py-2 font-medium">State</th>
                  <th className="px-3 py-2 font-medium">URL</th>
                </tr>
              </thead>
              <tbody>
                {(cameras.data ?? []).map((camera) => (
                  <tr key={camera.id}>
                    <td className="px-3 py-3 text-slate-300">{camera.streamName}</td>
                    <td className="px-3 py-3">{camera.name}</td>
                    <td className="px-3 py-3">
                      <Badge value={camera.state} />
                    </td>
                    <td className="max-w-96 truncate px-3 py-3 text-slate-500">{camera.redactedUrl}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </PanelBody>
      </Panel>
    </div>
  );
}
