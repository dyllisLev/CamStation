import {
  ChevronDown,
  Columns3,
  Expand,
  EyeOff,
  LayoutGrid,
  MonitorPlay,
  PanelRightClose,
  PanelRightOpen,
  Save,
  Settings,
  SkipBack,
} from "lucide-react";
import { useEffect, useState } from "react";
import type { ComponentType } from "react";
import { NavLink } from "react-router-dom";
import type { Camera } from "../../app/api";
import { useCameras } from "../../app/queries";
import { StatusDot } from "../../components/StatusDot";
import { cn, formatDate, liveUrl } from "../../lib/utils";

type Mode = "mse" | "webrtc";

const emptySlots = Array.from({ length: 7 }, (_, index) => index);

export function LiveWorkspace() {
  const cameras = useCameras();
  const rows = cameras.data ?? [];
  const streaming = rows.filter((camera) => camera.state === "streaming");
  const activeCamera = streaming[0] ?? rows[0];
  const [mode, setMode] = useState<Mode>("mse");
  const [sideVisible, setSideVisible] = useState(true);
  const [timelineVisible, setTimelineVisible] = useState(true);
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  const filled = rows.slice(0, 8);
  const wall = [
    ...filled,
    ...emptySlots.slice(0, Math.max(0, 8 - filled.length)).map((index) => ({
      id: `empty-${index}`,
    })),
  ];

  function requestFullscreen() {
    void document.documentElement.requestFullscreen?.();
  }

  return (
    <div className="flex min-h-svh flex-col bg-[#03090d] text-slate-100">
      <header className="flex min-h-16 flex-wrap items-center gap-3 border-b border-cyan-400/20 bg-[#071016] px-3 py-2 shadow-[0_1px_0_rgba(34,211,238,0.25)]">
        <NavLink to="/" className="flex items-center gap-3 pr-3">
          <div className="flex size-9 items-center justify-center rounded-md border border-cyan-400 bg-cyan-400/10 text-xs font-black text-cyan-300">
            CS
          </div>
          <div className="leading-tight">
            <div className="text-base font-bold">CamStation</div>
            <div className="text-xs font-medium text-slate-500">HOME MONITOR</div>
          </div>
        </NavLink>

        <div className="flex flex-wrap items-center gap-2">
          <ToolbarLink to="/live" active icon={MonitorPlay} label="라이브" />
          <ToolbarLink to="/recordings" icon={SkipBack} label="녹화" />
          <ToolbarLink to="/settings" icon={Settings} label="설정" />
          <button className="inline-flex h-10 items-center gap-2 rounded-md border border-slate-700 bg-slate-900 px-3 text-sm font-semibold text-slate-200">
            <LayoutGrid size={16} />
            기본
            <ChevronDown size={15} className="text-slate-500" />
          </button>
          <ToolbarButton icon={Save} label="배치 저장" />
          <ToolbarButton icon={Columns3} label="새 이름 저장" />
          <ToolbarButton
            icon={sideVisible ? PanelRightClose : PanelRightOpen}
            label={sideVisible ? "우측 패널 숨기기" : "우측 패널 보이기"}
            onClick={() => setSideVisible((value) => !value)}
          />
          <ToolbarButton
            icon={EyeOff}
            label={timelineVisible ? "타임라인 숨기기" : "타임라인 보이기"}
            onClick={() => setTimelineVisible((value) => !value)}
          />
        </div>

        <div className="ml-auto flex items-center gap-2">
          <span className="inline-flex h-9 items-center gap-2 rounded-full bg-red-500 px-4 text-sm font-bold text-white">
            <span className="size-2 rounded-full bg-white" />
            LIVE
          </span>
          <button
            type="button"
            onClick={requestFullscreen}
            className="inline-flex h-10 items-center gap-2 rounded-md border border-slate-700 bg-slate-900 px-3 text-sm font-semibold text-slate-200 hover:border-cyan-500/60"
          >
            <Expand size={16} />
            전체화면
          </button>
        </div>
      </header>

      <main
        className={cn(
          "grid min-h-0 flex-1 border-b border-slate-800",
          sideVisible ? "xl:grid-cols-[minmax(0,1fr)_340px]" : "grid-cols-1",
        )}
      >
        <section className="min-h-0 overflow-auto p-2">
          <div className="grid auto-rows-[minmax(164px,1fr)] gap-2 xl:grid-cols-4">
            {wall.map((item, index) =>
              "streamName" in item ? (
                <CameraTile
                  key={item.id}
                  camera={item}
                  mode={mode}
                  featured={index === 0}
                  selected={item.id === activeCamera?.id}
                />
              ) : (
                <EmptyTile key={item.id} featured={index === 0 && rows.length === 0} />
              ),
            )}
          </div>
        </section>

        {sideVisible && (
          <aside className="hidden min-h-0 space-y-4 overflow-auto border-l border-slate-800 bg-[#071016] p-4 xl:block">
            <section className="rounded-lg border border-slate-800 bg-slate-950/40 p-4">
              <div className="mb-3 flex items-center justify-between">
                <h2 className="text-sm font-bold">저장된 배치</h2>
                <span className="font-mono text-xs text-slate-500">1 profiles</span>
              </div>
              <button className="flex h-11 w-full items-center justify-between rounded-md border border-cyan-500 bg-cyan-500/15 px-3 text-sm font-semibold text-cyan-100">
                기본
                <span className="font-mono text-xs text-slate-400">{timeText(now, false)}</span>
              </button>
            </section>

            <section className="rounded-lg border border-slate-800 bg-slate-950/40 p-4">
              <div className="mb-3 flex items-center justify-between">
                <h2 className="text-sm font-bold">카메라 상태</h2>
                <span className="font-mono text-xs text-slate-500">
                  {streaming.length} / {rows.length} online
                </span>
              </div>
              <div className="space-y-2">
                {rows.map((camera) => (
                  <button
                    key={camera.id}
                    className={cn(
                      "flex h-11 w-full items-center justify-between gap-3 rounded-md border border-slate-800 bg-slate-950/60 px-3 text-left text-sm text-slate-300",
                      camera.id === activeCamera?.id && "border-cyan-500 bg-cyan-500/10 text-cyan-50",
                    )}
                  >
                    <span className="flex min-w-0 items-center gap-3">
                      <StatusDot status={camera.state} />
                      <span className="truncate font-semibold">{camera.name}</span>
                    </span>
                    <span className="font-mono text-xs text-slate-500">
                      {camera.state === "streaming" ? "live" : camera.state}
                    </span>
                  </button>
                ))}
                {rows.length === 0 && (
                  <div className="rounded-md border border-dashed border-slate-800 px-3 py-8 text-center text-sm text-slate-500">
                    등록된 카메라 없음
                  </div>
                )}
              </div>
            </section>

            <section className="rounded-lg border border-slate-800 bg-slate-950/40 p-4">
              <div className="mb-3 flex items-center justify-between">
                <h2 className="text-sm font-bold">재생 방식</h2>
                <span className="font-mono text-xs text-slate-500">{mode.toUpperCase()}</span>
              </div>
              <div className="grid grid-cols-2 gap-2">
                <button
                  type="button"
                  onClick={() => setMode("mse")}
                  className={cn(modeButtonClass, mode === "mse" && activeModeClass)}
                >
                  MSE
                </button>
                <button
                  type="button"
                  onClick={() => setMode("webrtc")}
                  className={cn(modeButtonClass, mode === "webrtc" && activeModeClass)}
                >
                  WebRTC
                </button>
              </div>
            </section>
          </aside>
        )}
      </main>

      {timelineVisible && (
        <footer className="border-t border-slate-800 bg-[#071016]">
          <div className="flex flex-wrap items-center gap-4 border-b border-slate-800 px-4 py-2">
            <div className="font-mono text-2xl font-black text-cyan-300">{timeText(now, true)}</div>
            <div className="text-sm text-slate-500">
              {formatDate(now.toISOString())} · 1줄 선택 카메라 · 2줄 전체 집계
            </div>
            <div className="ml-auto flex items-center gap-2">
              <span className="inline-flex h-8 items-center gap-2 rounded-full bg-red-500 px-4 text-xs font-bold text-white">
                <span className="size-2 rounded-full bg-white" />
                LIVE
              </span>
              <button
                type="button"
                onClick={() => setTimelineVisible(false)}
                className="h-9 rounded-md border border-cyan-500 px-3 text-sm font-semibold text-cyan-100"
              >
                타임라인 숨기기
              </button>
            </div>
          </div>
          <div className="grid gap-2 px-4 py-3 text-xs">
            <TimelineRow label={activeCamera?.name ?? "선택 카메라"} subLabel="선택 카메라" color="bg-cyan-500/70" />
            <TimelineRow label="전체 카메라" subLabel="녹화 없음" color="bg-emerald-500/60" />
            <div className="ml-40 hidden grid-cols-7 text-slate-600 md:grid">
              {["00", "04", "08", "12", "16", "20", "24"].map((hour) => (
                <span key={hour}>{hour}</span>
              ))}
            </div>
          </div>
        </footer>
      )}
    </div>
  );
}

function CameraTile({
  camera,
  mode,
  featured,
  selected,
}: {
  camera: Camera;
  mode: Mode;
  featured?: boolean;
  selected?: boolean;
}) {
  return (
    <article
      className={cn(
        "group relative min-h-0 overflow-hidden rounded-md border border-slate-800 bg-black",
        featured && "xl:col-span-2 xl:row-span-2",
        selected && "border-cyan-400 shadow-[0_0_0_1px_rgba(34,211,238,0.55)]",
      )}
    >
      <div className="absolute left-0 right-0 top-0 z-10 flex items-center justify-between bg-gradient-to-b from-black/80 to-transparent px-3 py-2">
        <div className="flex min-w-0 items-center gap-2">
          <StatusDot status={camera.state} />
          <span className="truncate text-sm font-bold text-white">{camera.name}</span>
        </div>
        <span className="truncate pl-3 text-xs font-semibold text-slate-300">{camera.streamName}</span>
      </div>
      {camera.state === "streaming" ? (
        <iframe
          title={`${camera.name} live`}
          src={liveUrl(camera.streamName, mode)}
          className="size-full min-h-[164px] border-0"
          allow="autoplay; fullscreen"
        />
      ) : (
        <div className="flex size-full min-h-[164px] items-center justify-center text-sm text-slate-500">
          {camera.state}
        </div>
      )}
    </article>
  );
}

function EmptyTile({ featured }: { featured?: boolean }) {
  return (
    <div
      className={cn(
        "flex min-h-[164px] items-center justify-center rounded-md border border-dashed border-slate-800 bg-black/50 text-sm text-slate-700",
        featured && "xl:col-span-2 xl:row-span-2",
      )}
    >
      빈 슬롯
    </div>
  );
}

function ToolbarLink({
  to,
  label,
  icon: Icon,
  active,
}: {
  to: string;
  label: string;
  icon: ComponentType<{ size?: number }>;
  active?: boolean;
}) {
  return (
    <NavLink
      to={to}
      className={({ isActive }) =>
        cn(
          "inline-flex h-10 items-center gap-2 rounded-md border px-3 text-sm font-semibold transition",
          active || isActive
            ? "border-cyan-500 bg-cyan-500/20 text-cyan-100"
            : "border-slate-700 bg-slate-900 text-slate-300 hover:border-cyan-500/60",
        )
      }
    >
      <Icon size={16} />
      {label}
    </NavLink>
  );
}

function ToolbarButton({
  icon: Icon,
  label,
  onClick,
}: {
  icon: ComponentType<{ size?: number }>;
  label: string;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex h-10 items-center gap-2 rounded-md border border-slate-700 bg-slate-900 px-3 text-sm font-semibold text-slate-300 transition hover:border-cyan-500/60"
    >
      <Icon size={16} />
      {label}
    </button>
  );
}

function TimelineRow({ label, subLabel, color }: { label: string; subLabel: string; color: string }) {
  return (
    <div className="grid items-center gap-3 md:grid-cols-[150px_minmax(0,1fr)]">
      <div className="min-w-0 text-right">
        <div className="truncate font-bold text-slate-200">{label}</div>
        <div className="truncate text-slate-500">{subLabel}</div>
      </div>
      <div className="h-7 overflow-hidden rounded-md border border-slate-800 bg-black">
        <div className={cn("h-full w-[56%] border-r border-cyan-200/70", color)} />
      </div>
    </div>
  );
}

function timeText(date: Date, withSeconds: boolean) {
  return new Intl.DateTimeFormat("ko-KR", {
    hour: "2-digit",
    minute: "2-digit",
    second: withSeconds ? "2-digit" : undefined,
    hour12: false,
  }).format(date);
}

const modeButtonClass =
  "h-9 rounded-md border border-slate-800 bg-slate-950 text-sm font-semibold text-slate-400 transition hover:border-cyan-500/60";
const activeModeClass = "border-cyan-500 bg-cyan-500/15 text-cyan-100";
