import { Loader2, Plus, RadioTower, RotateCcw, ScanSearch, ShieldCheck, SlidersHorizontal, Wifi } from "lucide-react";
import { type FormEvent, useState } from "react";
import { useCameras, useCreateCamera } from "../app/queries";
import { FeatureMatrix } from "../components/FeatureMatrix";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../components/ui/panel";
import { formatDate, formatDurationNanos } from "../lib/utils";

export function CamerasPage() {
  const cameras = useCameras();
  const createCamera = useCreateCamera();
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await createCamera.mutateAsync({ name: name || "Camera 1", url });
    setUrl("");
    if (!name) setName("");
  }

  return (
    <div className="space-y-4">
      <div className="grid gap-4 xl:grid-cols-[420px_1fr]">
        <Panel>
          <PanelHeader>
            <h2 className="text-sm font-semibold">Register Camera</h2>
          </PanelHeader>
          <PanelBody>
            <form className="space-y-4" onSubmit={onSubmit}>
              <label className="block space-y-2">
                <span className="text-xs font-medium text-slate-400">Name</span>
                <input
                  className="new-form-control"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder="Camera 1"
                />
              </label>
              <label className="block space-y-2">
                <span className="text-xs font-medium text-slate-400">RTSP URL</span>
                <input
                  className="new-form-control"
                  value={url}
                  onChange={(event) => setUrl(event.target.value)}
                  placeholder="rtsp://user:pass@host:554/stream"
                  type="password"
                  required
                />
              </label>
              {createCamera.error && (
                <div className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-200">
                  {createCamera.error.message}
                </div>
              )}
              <Button type="submit" variant="primary" className="w-full" disabled={createCamera.isPending}>
                {createCamera.isPending ? <Loader2 className="animate-spin" size={16} /> : <Plus size={16} />}
                Save and probe
              </Button>
            </form>
          </PanelBody>
        </Panel>

        <Panel>
          <PanelHeader>
            <h2 className="text-sm font-semibold">Registered Cameras</h2>
          </PanelHeader>
          <PanelBody>
            <div className="new-table-wrap">
              <table className="new-table">
                <thead>
                  <tr>
                    <th className="px-3 py-2 font-medium">Name</th>
                    <th className="px-3 py-2 font-medium">State</th>
                    <th className="px-3 py-2 font-medium">Probe</th>
                    <th className="px-3 py-2 font-medium">Updated</th>
                  </tr>
                </thead>
                <tbody>
                  {(cameras.data ?? []).map((camera) => {
                    const video = camera.lastProbe?.streams?.find((stream) => stream.type === "video");
                    return (
                      <tr key={camera.id}>
                        <td className="max-w-80 px-3 py-3">
                          <div className="font-medium">{camera.name}</div>
                          <div className="truncate text-xs text-slate-500">{camera.redactedUrl}</div>
                        </td>
                        <td className="px-3 py-3">
                          <Badge value={camera.state} />
                        </td>
                        <td className="px-3 py-3 text-slate-400">
                          <div className="flex items-center gap-2">
                            <RadioTower size={15} />
                            {formatDurationNanos(camera.lastProbe?.duration)}
                          </div>
                          <div className="text-xs text-slate-500">
                            {video ? `${video.codec} ${video.width}x${video.height}` : "no video stream"}
                          </div>
                        </td>
                        <td className="px-3 py-3 text-slate-500">{formatDate(camera.updatedAt)}</td>
                      </tr>
                    );
                  })}
                  {(cameras.data ?? []).length === 0 && (
                    <tr>
                      <td className="px-3 py-8 text-center text-slate-500" colSpan={4}>
                        No cameras registered.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </PanelBody>
        </Panel>
      </div>

      <FeatureMatrix
        title="Camera Operations"
        items={[
          { icon: SlidersHorizontal, title: "Profiles", status: "unknown", detail: "Generic, Imou, Hikvision, Dahua, Reolink, and VStarcam defaults need schema support." },
          { icon: Wifi, title: "Transport Policy", status: "unknown", detail: "auto/tcp/udp/http fallback should be owned by the connection engine." },
          { icon: ShieldCheck, title: "Keepalive", status: "unknown", detail: "OPTIONS and GET_PARAMETER keepalive modes need per-camera state." },
          { icon: ScanSearch, title: "ONVIF", status: "unknown", detail: "Discovery, reboot, and profile import are preserved from the previous system scope." },
          { icon: RotateCcw, title: "Runtime Apply", status: "unknown", detail: "Camera changes should regenerate streams without touching production services." },
        ]}
      />
    </div>
  );
}
