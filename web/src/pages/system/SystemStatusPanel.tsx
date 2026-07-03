import { Cpu, HardDrive, RefreshCw, ServerCog, Video } from "lucide-react";
import { useSystemStatus } from "../../app/streamsViewersSystemQueries";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { errorMessage, formatDate } from "./systemFormat";

export function SystemStatusPanel() {
  const status = useSystemStatus();
  const streamCount = Object.keys(status.data?.go2rtc.streams ?? {}).length;

  return (
    <Panel>
      <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">시스템 상태</h2>
          <div className="mt-1 text-xs text-slate-500">데몬, 스트림 런타임, ffmpeg, 프로세스 지표를 읽습니다.</div>
        </div>
        <Button size="sm" type="button" variant="secondary" onClick={() => void status.refetch()}>
          <RefreshCw size={15} />
          새로고침
        </Button>
      </PanelHeader>
      <PanelBody className="space-y-4">
        <div className="grid gap-3 md:grid-cols-4">
          <Metric icon={ServerCog} label="데몬" state={status.data?.daemon.running ? "running" : "offline"} value={status.data?.daemon.running ? "running" : "offline"} />
          <Metric icon={Video} label="스트림" state={status.data?.go2rtc.running ? "running" : "offline"} value={`${streamCount} streams`} />
          <Metric icon={HardDrive} label="ffmpeg" state={status.data?.ffmpeg.installed ? "running" : "warning"} value={status.data?.ffmpeg.installed ? "installed" : "missing"} />
          <Metric icon={Cpu} label="런타임" state="info" value={`${status.data?.system.goroutines ?? 0} goroutines`} />
        </div>
        <div className="grid gap-2 text-xs text-slate-500 md:grid-cols-3">
          <span>시각: {formatDate(status.data?.daemon.now)}</span>
          <span>OS: {status.data ? `${status.data.system.goos}/${status.data.system.goarch}` : "-"}</span>
          <span>CPU: {status.data?.system.cpus ?? "-"}</span>
        </div>
        {status.isLoading && <div className="text-xs text-slate-500">상태를 불러오는 중입니다.</div>}
        {status.error && <div className="text-xs text-red-300">{errorMessage(status.error)}</div>}
        {status.data?.go2rtc.error && <div className="text-xs text-red-300">스트림 오류: {status.data.go2rtc.error}</div>}
        {status.data?.ffmpeg.error && <div className="text-xs text-red-300">ffmpeg 오류: {status.data.ffmpeg.error}</div>}
      </PanelBody>
    </Panel>
  );
}

function Metric({
  icon: Icon,
  label,
  state,
  value,
}: {
  readonly icon: typeof ServerCog;
  readonly label: string;
  readonly state: string;
  readonly value: string;
}) {
  return (
    <div className="new-feature-card flex items-center justify-between gap-3">
      <div>
        <div className="text-xs text-slate-500">{label}</div>
        <div className="mt-1 text-sm font-semibold text-slate-100">{value}</div>
      </div>
      <div className="flex items-center gap-2">
        <Badge className="shrink-0 whitespace-nowrap" value={state} />
        <div className="new-feature-icon"><Icon size={17} /></div>
      </div>
    </div>
  );
}
