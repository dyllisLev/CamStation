import {
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronUp,
  House,
  LoaderCircle,
  MapPin,
  Navigation,
  Save,
  Trash2,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import type { ChangeEvent, ReactNode } from "react";
import type { Camera, PTZMoveVector } from "../../app/api";
import {
  useCameraControls,
  useCameraPresets,
  useCreateCameraPreset,
  useDeleteCameraPreset,
  useGotoCameraHome,
  useGotoCameraPreset,
  useSetCameraHome,
} from "../../app/queries";
import { updatePtzPressSources } from "./ptzPressState";
import type { PtzPressSource } from "./ptzPressState";
import { usePtzHold } from "./usePtzHold";

type PtzControlPanelProps = {
  readonly camera: Camera;
  readonly onBack: () => void;
  readonly onStopReady: (stop: () => Promise<void>) => void;
};

type HoldButtonProps = {
  readonly label: string;
  readonly move: PTZMoveVector;
  readonly className: string;
  readonly direction?: "up" | "down" | "left" | "right";
  readonly onStart: (move: PTZMoveVector) => void;
  readonly onStop: () => void;
  readonly children: ReactNode;
};

function HoldButton({ label, move, className, direction, onStart, onStop, children }: HoldButtonProps) {
  const pointerActive = useRef(false);
  const keyboardActive = useRef(false);
  const pressedSources = useRef<ReadonlySet<PtzPressSource>>(new Set());
  const [pressed, setPressed] = useState(false);
  const begin = () => onStart(move);
  const end = onStop;
  const setSourcePressed = (source: PtzPressSource, active: boolean) => {
    pressedSources.current = updatePtzPressSources(pressedSources.current, source, active);
    setPressed(pressedSources.current.size > 0);
  };
  return (
    <button
      type="button"
      className={className}
      data-direction={direction}
      data-pressed={pressed}
      aria-pressed={pressed}
      aria-label={label}
      onPointerDown={(event) => {
        if (event.button !== 0) return;
        event.currentTarget.setPointerCapture(event.pointerId);
        pointerActive.current = true;
        setSourcePressed("pointer", true);
        begin();
      }}
      onPointerUp={() => {
        if (pointerActive.current) end();
        pointerActive.current = false;
        setSourcePressed("pointer", false);
      }}
      onPointerCancel={() => {
        if (pointerActive.current) end();
        pointerActive.current = false;
        setSourcePressed("pointer", false);
      }}
      onPointerLeave={() => {
        if (pointerActive.current) end();
        pointerActive.current = false;
        setSourcePressed("pointer", false);
      }}
      onLostPointerCapture={() => {
        if (pointerActive.current) end();
        pointerActive.current = false;
        setSourcePressed("pointer", false);
      }}
      onKeyDown={(event) => {
        if ((event.key === " " || event.key === "Enter") && !keyboardActive.current) {
          event.preventDefault();
          keyboardActive.current = true;
          setSourcePressed("keyboard", true);
          begin();
        }
      }}
      onKeyUp={(event) => {
        if ((event.key === " " || event.key === "Enter") && keyboardActive.current) {
          event.preventDefault();
          keyboardActive.current = false;
          setSourcePressed("keyboard", false);
          end();
        }
      }}
      onBlur={() => {
        if (keyboardActive.current) end();
        keyboardActive.current = false;
        setSourcePressed("keyboard", false);
      }}
    >
      {children}
    </button>
  );
}

function ActionContent({ pending, icon, children }: { pending: boolean; icon: ReactNode; children: ReactNode }) {
  return (
    <>
      <span className="new-ptz-action-icon" aria-hidden="true">
        {pending ? <LoaderCircle className="new-ptz-spinner" /> : icon}
      </span>
      <span>{children}</span>
    </>
  );
}

export function PtzControlPanel({ camera, onBack, onStopReady }: PtzControlPanelProps) {
  const [speed, setSpeed] = useState(0.6);
  const [presetName, setPresetName] = useState("");
  const [error, setError] = useState("");
  const handleError = useCallback((message: string) => setError(message), []);
  const { start, stop } = usePtzHold(camera.streamName, handleError);
  const controls = camera.controlCapabilities;
  const controlQuery = useCameraControls(camera.streamName, true);
  const presetsQuery = useCameraPresets(camera.streamName, controls.presets.available);
  const gotoHome = useGotoCameraHome();
  const setHome = useSetCameraHome();
  const createPreset = useCreateCameraPreset();
  const gotoPreset = useGotoCameraPreset();
  const deletePreset = useDeleteCameraPreset();

  useEffect(() => {
    onStopReady(stop);
  }, [onStopReady, stop]);

  const mutationError = (value: unknown) =>
    handleError(value instanceof Error ? value.message : "카메라 제어 요청에 실패했습니다.");
  const handleSpeed = (event: ChangeEvent<HTMLInputElement>) => setSpeed(Number(event.currentTarget.value));
  const handleBack = async () => {
    await stop();
    onBack();
  };
  const holdStop = () => {
    void stop();
  };

  const homeActionButtons = (
    <>
      <header className="new-ptz-card-title">
        <h3>위치 / 홈</h3>
      </header>
      <button
        type="button"
        className="new-ptz-action new-ptz-home-action"
        disabled={!controls.home.available || gotoHome.isPending}
        data-pending={gotoHome.isPending}
        aria-busy={gotoHome.isPending}
        onClick={() => gotoHome.mutate({ streamName: camera.streamName }, { onError: mutationError })}
      >
        <ActionContent pending={gotoHome.isPending} icon={<House />}>홈으로 이동</ActionContent>
      </button>
      <button
        type="button"
        className="new-ptz-action new-ptz-home-action"
        disabled={!controls.home.available || setHome.isPending}
        data-pending={setHome.isPending}
        aria-busy={setHome.isPending}
        onClick={() => {
          if (window.confirm("현재 카메라 위치를 홈으로 설정할까요?")) {
            setHome.mutate({ streamName: camera.streamName }, { onError: mutationError });
          }
        }}
      >
        <ActionContent pending={setHome.isPending} icon={<MapPin />}>현재 위치를 홈으로 설정</ActionContent>
      </button>
    </>
  );

  const presetControls = (
    <>
      <header className="new-ptz-card-title">
        <h3>프리셋</h3>
        <span>
          {presetsQuery.data?.length ?? 0} / {controls.maxPresets ?? "-"}
        </span>
      </header>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          const name = presetName.trim();
          if (!name) return;
          createPreset.mutate(
            { streamName: camera.streamName, name },
            { onSuccess: () => setPresetName(""), onError: mutationError },
          );
        }}
      >
        <input
          value={presetName}
          maxLength={64}
          onChange={(event) => setPresetName(event.target.value)}
          aria-label="새 프리셋 이름"
          placeholder="프리셋 이름"
        />
        <button
          type="submit"
          className="new-ptz-action new-ptz-save-action"
          disabled={!controls.presets.available || !presetName.trim() || createPreset.isPending}
          data-pending={createPreset.isPending}
          aria-busy={createPreset.isPending}
        >
          <ActionContent pending={createPreset.isPending} icon={<Save />}>현재 위치 저장</ActionContent>
        </button>
      </form>
      <ul className="new-ptz-preset-list">
        {(presetsQuery.data ?? []).map((preset) => {
          const moving = gotoPreset.isPending && gotoPreset.variables?.token === preset.token;
          const deleting = deletePreset.isPending && deletePreset.variables?.token === preset.token;
          return (
            <li key={preset.token}>
              <span>{preset.name || "이름 없는 프리셋"}</span>
              <button
                type="button"
                className="new-ptz-action new-ptz-preset-action"
                disabled={gotoPreset.isPending}
                data-pending={moving}
                aria-busy={moving}
                onClick={() =>
                  gotoPreset.mutate(
                    { streamName: camera.streamName, token: preset.token },
                    { onError: mutationError },
                  )
                }
              >
                <ActionContent pending={moving} icon={<Navigation />}>이동</ActionContent>
              </button>
              <button
                type="button"
                className="new-ptz-action new-ptz-preset-action new-ptz-danger-action"
                disabled={deletePreset.isPending}
                data-pending={deleting}
                aria-busy={deleting}
                onClick={() => {
                  if (window.confirm(`‘${preset.name || "이름 없는 프리셋"}’ 프리셋을 삭제할까요?`)) {
                    deletePreset.mutate(
                      { streamName: camera.streamName, token: preset.token },
                      { onError: mutationError },
                    );
                  }
                }}
              >
                <ActionContent pending={deleting} icon={<Trash2 />}>삭제</ActionContent>
              </button>
            </li>
          );
        })}
      </ul>
    </>
  );

  const disabledFeatureButtons = (
    <>
      {[
        { key: "listen", label: "소리 듣기", reason: "오디오 경로 준비 필요", capability: controls.listen },
        { key: "talk", label: "말하기", reason: "표준 제어 미확인", capability: controls.talk },
        { key: "siren", label: "사이렌", reason: "프로토콜 미확인", capability: controls.siren },
      ].map((feature) => (
        <button
          key={feature.key}
          type="button"
          className="new-ptz-feature"
          data-support={feature.capability.support}
          disabled
          title={feature.reason}
        >
          {feature.label}
          <small>{feature.reason}</small>
        </button>
      ))}
    </>
  );

  return (
    <section className="new-ptz-panel" aria-label={`${camera.name} PTZ 제어`}>
      <header className="new-ptz-header">
        <button type="button" className="new-icon-button" onClick={() => void handleBack()} aria-label="PTZ 제어 닫기">
          <ChevronLeft />
        </button>
        <div>
          <strong>PTZ 제어</strong>
          <em>{camera.name} · ONVIF</em>
        </div>
        <span
          className="new-state"
          aria-label="PTZ 준비됨"
          title={`팬/틸트 ${controlQuery.data?.status.panTilt ?? "UNKNOWN"} · 줌 ${controlQuery.data?.status.zoom ?? "UNKNOWN"}`}
        />
      </header>
      {error && (
        <p className="new-ptz-error" role="alert">
          {error}
        </p>
      )}
      <div className="new-ptz-pad" role="group" aria-label="팬 틸트 방향">
        <HoldButton
          label="위"
          direction="up"
          move={{ pan: 0, tilt: speed, zoom: 0 }}
          className="new-ptz-direction"
          onStart={start}
          onStop={holdStop}
        >
          <ChevronUp />
        </HoldButton>
        <HoldButton
          label="왼쪽"
          direction="left"
          move={{ pan: -speed, tilt: 0, zoom: 0 }}
          className="new-ptz-direction"
          onStart={start}
          onStop={holdStop}
        >
          <ChevronLeft />
        </HoldButton>
        <button type="button" className="new-ptz-stop-center" onClick={holdStop} aria-label="즉시 정지">
          ■
        </button>
        <HoldButton
          label="오른쪽"
          direction="right"
          move={{ pan: speed, tilt: 0, zoom: 0 }}
          className="new-ptz-direction"
          onStart={start}
          onStop={holdStop}
        >
          <ChevronRight />
        </HoldButton>
        <HoldButton
          label="아래"
          direction="down"
          move={{ pan: 0, tilt: -speed, zoom: 0 }}
          className="new-ptz-direction"
          onStart={start}
          onStop={holdStop}
        >
          <ChevronDown />
        </HoldButton>
      </div>
      <div className="new-ptz-zoom" role="group" aria-label="줌">
        <HoldButton
          label="확대"
          move={{ pan: 0, tilt: 0, zoom: speed }}
          className="new-ptz-zoom-button"
          onStart={start}
          onStop={holdStop}
        >
          <ZoomIn /> 확대
        </HoldButton>
        <HoldButton
          label="축소"
          move={{ pan: 0, tilt: 0, zoom: -speed }}
          className="new-ptz-zoom-button"
          onStart={start}
          onStop={holdStop}
        >
          <ZoomOut /> 축소
        </HoldButton>
      </div>
      <label className="new-ptz-speed">
        이동 속도 <output>{Math.round(speed * 100)}%</output>
        <input type="range" min="0.2" max="1" step="0.1" value={speed} onChange={handleSpeed} />
      </label>
      <button
        type="button"
        className="new-ptz-action new-ptz-emergency new-ptz-danger-action"
        onClick={holdStop}
      >
        ■ 즉시 정지
      </button>
      <div className="new-ptz-card new-ptz-home">{homeActionButtons}</div>
      <div className="new-ptz-card">{presetControls}</div>
      <div className="new-ptz-card new-ptz-features">{disabledFeatureButtons}</div>
    </section>
  );
}
