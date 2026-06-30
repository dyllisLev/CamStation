import { ExternalLink, MonitorPlay } from "lucide-react";
import { useState } from "react";
import { useCameras } from "../app/queries";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../components/ui/panel";
import { liveUrl } from "../lib/utils";

type Mode = "mse" | "webrtc";

export function LivePage() {
  const cameras = useCameras();
  const [mode, setMode] = useState<Mode>("mse");
  const rows = cameras.data ?? [];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant={mode === "mse" ? "primary" : "secondary"}
            onClick={() => setMode("mse")}
          >
            MSE
          </Button>
          <Button
            type="button"
            variant={mode === "webrtc" ? "primary" : "secondary"}
            onClick={() => setMode("webrtc")}
          >
            WebRTC
          </Button>
        </div>
        <div className="text-sm text-slate-500">{rows.length} registered cameras</div>
      </div>

      <div className="grid gap-4 xl:grid-cols-2">
        {rows.map((camera) => (
          <Panel key={camera.id} className="overflow-hidden">
            <PanelHeader className="flex items-center justify-between gap-3">
              <div className="min-w-0">
                <h2 className="truncate text-sm font-semibold">{camera.name}</h2>
                <p className="truncate text-xs text-slate-500">{camera.redactedUrl}</p>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <Badge value={camera.state} />
                <Button asChild size="sm" variant="ghost">
                  <a href={liveUrl(camera.streamName, mode)} target="_blank" rel="noreferrer">
                    <ExternalLink size={15} />
                  </a>
                </Button>
              </div>
            </PanelHeader>
            <div className="aspect-video bg-black">
              {camera.state === "streaming" ? (
                <iframe
                  title={`${camera.name} live`}
                  src={liveUrl(camera.streamName, mode)}
                  className="size-full border-0"
                  allow="autoplay; fullscreen"
                />
              ) : (
                <div className="flex size-full items-center justify-center text-sm text-slate-500">
                  Camera is {camera.state}
                </div>
              )}
            </div>
          </Panel>
        ))}
        {rows.length === 0 && (
          <Panel className="xl:col-span-2">
            <PanelBody className="flex min-h-72 flex-col items-center justify-center gap-3 text-slate-500">
              <MonitorPlay size={34} />
              <p>No cameras are ready for live view.</p>
            </PanelBody>
          </Panel>
        )}
      </div>
    </div>
  );
}

