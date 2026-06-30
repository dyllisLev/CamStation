import { AlertTriangle, Camera, Clock, Database, HardDrive, RadioTower } from "lucide-react";
import { useCameras, useEvents, useHealth, useStreamStatus } from "../app/queries";
import { StatusDot } from "../components/StatusDot";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../components/ui/panel";
import { formatDate, liveUrl } from "../lib/utils";

export function ControlRoomPage() {
  const cameras = useCameras();
  const events = useEvents();
  const health = useHealth();
  const streams = useStreamStatus();
  const cameraRows = cameras.data ?? [];
  const eventRows = events.data ?? [];
  const active = cameraRows.filter((camera) => camera.state === "streaming");
  const attention = cameraRows.filter((camera) => camera.state !== "streaming");
  const primary = active[0] ?? cameraRows[0];
  const errors = eventRows.filter((event) => event.level === "error");

  return (
    <div className="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_380px]">
      <div className="space-y-4">
        <section className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_280px]">
          <Panel className="overflow-hidden">
            <PanelHeader className="flex items-center justify-between gap-3">
              <div>
                <h2 className="text-sm font-semibold">{primary?.name ?? "No live camera"}</h2>
                <p className="text-xs text-slate-500">{primary?.redactedUrl ?? "Waiting for camera registration"}</p>
              </div>
              {primary && <Badge value={primary.state} />}
            </PanelHeader>
            <div className="aspect-video bg-black">
              {primary?.state === "streaming" ? (
                <iframe
                  title={`${primary.name} control room live`}
                  src={liveUrl(primary.streamName, "mse")}
                  className="size-full border-0"
                  allow="autoplay; fullscreen"
                />
              ) : (
                <div className="flex size-full items-center justify-center text-sm text-slate-500">
                  No streaming camera selected.
                </div>
              )}
            </div>
          </Panel>

          <div className="grid gap-4">
            <SignalCard icon={Camera} label="Cameras" value={`${active.length}/${cameraRows.length}`} state={attention.length ? "degraded" : "streaming"} />
            <SignalCard icon={AlertTriangle} label="Incidents" value={String(errors.length)} state={errors.length ? "error" : "ok"} />
            <SignalCard icon={RadioTower} label="Stream Core" value={streams.data?.running ? "Running" : "Stopped"} state={streams.data?.running ? "running" : "offline"} />
            <SignalCard icon={Clock} label="API" value={health.data?.ok ? "Online" : "Checking"} state={health.data?.ok ? "ok" : "unknown"} />
          </div>
        </section>

        <Panel>
          <PanelHeader className="flex items-center justify-between gap-3">
            <h2 className="text-sm font-semibold">Camera Wall</h2>
            <span className="text-xs text-slate-500">{active.length} live feeds</span>
          </PanelHeader>
          <PanelBody>
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
              {cameraRows.map((camera) => (
                <div key={camera.id} className="rounded-md border border-slate-800 bg-slate-950">
                  <div className="flex items-center justify-between gap-2 border-b border-slate-800 px-3 py-2">
                    <div className="min-w-0">
                      <div className="truncate text-sm font-medium">{camera.name}</div>
                      <div className="truncate text-xs text-slate-500">{camera.streamName}</div>
                    </div>
                    <Badge value={camera.state} />
                  </div>
                  <div className="aspect-video bg-black">
                    {camera.state === "streaming" ? (
                      <iframe
                        title={`${camera.name} wall live`}
                        src={liveUrl(camera.streamName, "mse")}
                        className="size-full border-0"
                        allow="autoplay; fullscreen"
                      />
                    ) : (
                      <div className="flex size-full items-center justify-center text-xs text-slate-500">
                        {camera.state}
                      </div>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </PanelBody>
        </Panel>
      </div>

      <aside className="space-y-4">
        <Panel>
          <PanelHeader>
            <h2 className="text-sm font-semibold">Operations</h2>
          </PanelHeader>
          <PanelBody className="grid gap-2">
            <OperationRow icon={HardDrive} label="Recording" value="worker pending" />
            <OperationRow icon={Database} label="Retention" value="policy pending" />
            <OperationRow icon={RadioTower} label="go2rtc" value={streams.data?.running ? "healthy" : "offline"} />
          </PanelBody>
        </Panel>

        <Panel>
          <PanelHeader>
            <h2 className="text-sm font-semibold">Recent Events</h2>
          </PanelHeader>
          <PanelBody className="space-y-3">
            {eventRows.slice(0, 8).map((event) => (
              <div key={event.id} className="border-b border-slate-900 pb-3 last:border-0 last:pb-0">
                <div className="flex items-center justify-between gap-3">
                  <span className="truncate text-sm">{event.message}</span>
                  <Badge value={event.level} />
                </div>
                <div className="mt-1 flex items-center justify-between gap-3 text-xs text-slate-500">
                  <span>{event.source}</span>
                  <span>{formatDate(event.createdAt)}</span>
                </div>
              </div>
            ))}
          </PanelBody>
        </Panel>

        <Button asChild variant="primary" className="w-full">
          <a href="/live">Open Live Console</a>
        </Button>
      </aside>
    </div>
  );
}

type SignalCardProps = {
  icon: React.ComponentType<{ size?: number }>;
  label: string;
  value: string;
  state: string;
};

function SignalCard({ icon: Icon, label, value, state }: SignalCardProps) {
  return (
    <Panel>
      <PanelBody className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-md bg-slate-900 text-sky-300">
            <Icon size={18} />
          </div>
          <div>
            <div className="text-xs text-slate-500">{label}</div>
            <div className="text-base font-semibold">{value}</div>
          </div>
        </div>
        <StatusDot status={state} />
      </PanelBody>
    </Panel>
  );
}

function OperationRow({
  icon: Icon,
  label,
  value,
}: {
  icon: React.ComponentType<{ size?: number }>;
  label: string;
  value: string;
}) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-md border border-slate-900 px-3 py-2">
      <span className="flex items-center gap-2 text-sm text-slate-300">
        <Icon size={15} />
        {label}
      </span>
      <span className="text-xs text-slate-500">{value}</span>
    </div>
  );
}

