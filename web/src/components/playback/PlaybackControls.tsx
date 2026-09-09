import { Maximize2, Minimize2, Pause, Play, Radio, RotateCcw, RotateCw, Volume2, VolumeX } from "lucide-react";
import type { ReactNode } from "react";

import "./playback-controls.css";

export type PlaybackControlsWorkspace = {
  readonly mode: "live" | "playback";
  readonly playheadMs: number | null;
  readonly playing: boolean;
  readonly rate: number;
  readonly volume: number;
  readonly muted: boolean;
  readonly hasSelection: boolean;
  readonly togglePlaying: () => void;
  readonly step: (deltaSeconds: number) => void;
  readonly setRate: (rate: number) => void;
  readonly setVolume: (volume: number) => void;
  readonly setMuted: (muted: boolean) => void;
  readonly goLive: () => void;
};

export type PlaybackControlsProps = {
  readonly workspace: PlaybackControlsWorkspace;
  readonly liveEnabled?: boolean;
  readonly onToggleFullscreen?: () => void;
  readonly fullscreen?: boolean;
  readonly className?: string;
};

const KST_CLOCK = new Intl.DateTimeFormat("ko-KR", {
  timeZone: "Asia/Seoul",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
});

export function PlaybackControls({
  workspace,
  liveEnabled = false,
  onToggleFullscreen,
  fullscreen = false,
  className,
}: PlaybackControlsProps) {
  const active = workspace.mode === "playback" && workspace.hasSelection;
  const rootClassName = ["playback-controls", className].filter(Boolean).join(" ");
  const modeCopy = workspace.mode === "live"
    ? "LIVE"
    : workspace.playheadMs == null
      ? "녹화 재생"
      : `녹화 재생 · ${KST_CLOCK.format(new Date(workspace.playheadMs))} KST`;

  return (
    <div className={rootClassName} aria-label="재생 제어">
      <output className="playback-controls__mode">{modeCopy}</output>
      <div className="playback-controls__buttons">
        <ControlButton
          label={workspace.playing ? "일시정지" : "재생"}
          disabled={!active}
          onClick={workspace.togglePlaying}
        >
          {workspace.playing ? <Pause aria-hidden="true" /> : <Play aria-hidden="true" />}
        </ControlButton>
        <ControlButton label="10초 이전" disabled={!active} onClick={() => workspace.step(-10)}>
          <RotateCcw aria-hidden="true" />
          <span>10</span>
        </ControlButton>
        <ControlButton label="10초 이후" disabled={!active} onClick={() => workspace.step(10)}>
          <RotateCw aria-hidden="true" />
          <span>10</span>
        </ControlButton>
      </div>

      <label className="playback-controls__rate">
        <span className="playback-controls__sr-only">재생 속도</span>
        <select
          value={workspace.rate}
          disabled={!active}
          aria-label="재생 속도"
          onChange={(event) => workspace.setRate(Number(event.target.value))}
        >
          <option value={0.25}>0.25×</option>
          <option value={0.5}>0.5×</option>
          <option value={1}>1×</option>
          <option value={1.5}>1.5×</option>
          <option value={2}>2×</option>
          <option value={4}>4×</option>
        </select>
      </label>

      <div className="playback-controls__volume">
        <ControlButton
          label={workspace.muted ? "음소거 해제" : "음소거"}
          disabled={!active}
          pressed={workspace.muted}
          onClick={() => workspace.setMuted(!workspace.muted)}
        >
          {workspace.muted ? <VolumeX aria-hidden="true" /> : <Volume2 aria-hidden="true" />}
        </ControlButton>
        <label>
          <span className="playback-controls__sr-only">음량</span>
          <input
            type="range"
            min="0"
            max="1"
            step="0.05"
            value={workspace.volume}
            disabled={!active}
            aria-label="음량"
            onChange={(event) => workspace.setVolume(Number(event.target.value))}
          />
        </label>
      </div>

      <div className="playback-controls__spacer" />
      {onToggleFullscreen && (
        <ControlButton label={fullscreen ? "전체화면 종료" : "전체화면"} disabled={!active} onClick={onToggleFullscreen}>
          {fullscreen ? <Minimize2 aria-hidden="true" /> : <Maximize2 aria-hidden="true" />}
        </ControlButton>
      )}
      {liveEnabled && (
        <button
          type="button"
          className={["playback-controls__live", workspace.mode === "live" && "playback-controls__live--active"].filter(Boolean).join(" ")}
          aria-pressed={workspace.mode === "live"}
          onClick={workspace.goLive}
        >
          <Radio aria-hidden="true" />
          LIVE
        </button>
      )}
    </div>
  );
}

function ControlButton({
  label,
  disabled,
  pressed,
  onClick,
  children,
}: {
  readonly label: string;
  readonly disabled?: boolean;
  readonly pressed?: boolean;
  readonly onClick: () => void;
  readonly children: ReactNode;
}) {
  return (
    <button
      type="button"
      className="playback-controls__button"
      aria-label={label}
      title={label}
      aria-pressed={pressed}
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </button>
  );
}
