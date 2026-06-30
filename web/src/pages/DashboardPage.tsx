import { AlertTriangle, Camera, RadioTower, Server } from "lucide-react";
import { Bar, BarChart, ResponsiveContainer, Tooltip, XAxis } from "recharts";
import { useCameras, useEvents, useHealth, useStreamStatus } from "../app/queries";
import { Badge } from "../components/ui/badge";
import { Panel, PanelBody, PanelHeader } from "../components/ui/panel";
import { formatDate } from "../lib/utils";

export function DashboardPage() {
  const health = useHealth();
  const cameras = useCameras();
  const events = useEvents();
  const streams = useStreamStatus();
  const rows = cameras.data ?? [];
  const online = rows.filter((camera) => camera.state === "streaming").length;
  const offline = rows.filter((camera) => camera.state !== "streaming").length;
  const recentEvents = events.data ?? [];
  const chartData = ["info", "warning", "error"].map((level) => ({
    level,
    count: recentEvents.filter((event) => event.level === level).length,
  }));

  return (
    <div className="space-y-5">
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <Metric icon={Camera} label="Cameras" value={`${online}/${rows.length}`} detail={`${offline} attention`} />
        <Metric
          icon={RadioTower}
          label="go2rtc"
          value={streams.data?.running ? "Running" : "Stopped"}
          detail={streams.data?.apiUrl ?? "checking"}
        />
        <Metric
          icon={Server}
          label="API"
          value={health.data?.ok ? "Healthy" : "Checking"}
          detail={health.data ? `mode ${health.data.mode}` : "waiting"}
        />
        <Metric
          icon={AlertTriangle}
          label="Recent Errors"
          value={String(recentEvents.filter((event) => event.level === "error").length)}
          detail="last 100 events"
        />
      </div>

      <div className="grid gap-4 xl:grid-cols-[1fr_420px]">
        <Panel>
          <PanelHeader>
            <h2 className="text-sm font-semibold">Camera Health</h2>
          </PanelHeader>
          <PanelBody>
            <div className="overflow-hidden rounded-md border border-slate-800">
              <table className="w-full text-left text-sm">
                <thead className="bg-slate-900 text-xs text-slate-500">
                  <tr>
                    <th className="px-3 py-2 font-medium">Name</th>
                    <th className="px-3 py-2 font-medium">State</th>
                    <th className="px-3 py-2 font-medium">Stream</th>
                    <th className="px-3 py-2 font-medium">Video</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-800">
                  {rows.map((camera) => {
                    const video = camera.lastProbe?.streams?.find((stream) => stream.type === "video");
                    return (
                      <tr key={camera.id}>
                        <td className="px-3 py-3 font-medium">{camera.name}</td>
                        <td className="px-3 py-3">
                          <Badge value={camera.state} />
                        </td>
                        <td className="px-3 py-3 text-slate-400">{camera.streamName}</td>
                        <td className="px-3 py-3 text-slate-400">
                          {video ? `${video.codec} ${video.width ?? "-"}x${video.height ?? "-"}` : "-"}
                        </td>
                      </tr>
                    );
                  })}
                  {rows.length === 0 && (
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

        <Panel>
          <PanelHeader>
            <h2 className="text-sm font-semibold">Event Mix</h2>
          </PanelHeader>
          <PanelBody>
            <div className="h-48">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={chartData}>
                  <XAxis dataKey="level" stroke="#64748b" fontSize={12} />
                  <Tooltip
                    cursor={{ fill: "rgba(15,23,42,0.75)" }}
                    contentStyle={{ background: "#020617", border: "1px solid #1e293b" }}
                  />
                  <Bar dataKey="count" fill="#38bdf8" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </div>
            <div className="mt-4 space-y-2">
              {recentEvents.slice(0, 5).map((event) => (
                <div key={event.id} className="flex items-center justify-between gap-3 text-sm">
                  <span className="truncate text-slate-300">{event.message}</span>
                  <span className="shrink-0 text-xs text-slate-500">{formatDate(event.createdAt)}</span>
                </div>
              ))}
            </div>
          </PanelBody>
        </Panel>
      </div>
    </div>
  );
}

type MetricProps = {
  icon: React.ComponentType<{ size?: number; className?: string }>;
  label: string;
  value: string;
  detail: string;
};

function Metric({ icon: Icon, label, value, detail }: MetricProps) {
  return (
    <Panel>
      <PanelBody className="flex items-center gap-3">
        <div className="flex size-10 items-center justify-center rounded-md bg-slate-900 text-sky-300">
          <Icon size={19} />
        </div>
        <div className="min-w-0">
          <div className="text-xs text-slate-500">{label}</div>
          <div className="truncate text-lg font-semibold">{value}</div>
          <div className="truncate text-xs text-slate-500">{detail}</div>
        </div>
      </PanelBody>
    </Panel>
  );
}

