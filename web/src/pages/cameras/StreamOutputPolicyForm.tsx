import type { CameraSourceKey, CameraStreamOutput, StreamOutputSettings, StreamOutputSettingsTuple } from "../../app/api";
import { purposeLabel } from "./streamOutputPolicyModel";

type StreamOutputPolicyFormProps = {
  outputs: StreamOutputSettingsTuple;
  statusOutputs?: readonly CameraStreamOutput[];
  onChange: (outputs: StreamOutputSettingsTuple) => void;
  disabled?: boolean;
  availableSourceKeys?: readonly CameraSourceKey[];
};

export function StreamOutputPolicyForm({ outputs, statusOutputs = [], onChange, disabled = false, availableSourceKeys = ["recording", "live"] }: StreamOutputPolicyFormProps) {
  function update(index: number, patch: Partial<StreamOutputSettings>) {
    const next = outputs.map((item, itemIndex) => itemIndex === index ? { ...item, ...patch } : item) as StreamOutputSettingsTuple;
    onChange(next);
  }

  return (
    <div className="new-policy-grid">
      {outputs.map((output, index) => (
        <StreamPolicyCard
          key={output.purpose}
          output={output}
          status={statusOutputs.find((item) => item.purpose === output.purpose)}
          disabled={disabled}
          availableSourceKeys={availableSourceKeys}
          onChange={(patch) => update(index, patch)}
        />
      ))}
    </div>
  );
}

function StreamPolicyCard({
  output,
  status,
  disabled,
  onChange,
  availableSourceKeys,
}: {
  output: StreamOutputSettings;
  status?: CameraStreamOutput;
  disabled: boolean;
  onChange: (patch: Partial<StreamOutputSettings>) => void;
  availableSourceKeys: readonly CameraSourceKey[];
}) {
  const limitedSize = output.maxWidth !== null && output.maxHeight !== null;
  const applied = status?.applied;
  const effective = status?.effective;
  return (
    <section className="new-policy-card" data-purpose={output.purpose}>
      <div className="new-policy-card-head">
        <div>
          <strong>{purposeLabel(output.purpose)}</strong>
          <span>{status?.streamName || "등록 후 출력 이름 생성"}</span>
        </div>
        <span className={`new-policy-state new-${status?.verification.state || "unverified"}`}>
          {verificationLabel(status?.verification.state)}
        </span>
      </div>

      <div className="new-policy-controls">
        <PolicySelect label="원본 입력" value={output.sourceKey} disabled={disabled} onChange={(value) => onChange({ sourceKey: value as StreamOutputSettings["sourceKey"] })}>
          {sourceOptions(availableSourceKeys, output.sourceKey).map((sourceKey) => (
            <option key={sourceKey} value={sourceKey} disabled={!availableSourceKeys.includes(sourceKey)}>{sourceKey === "recording" ? "녹화 입력" : "라이브 입력"}{availableSourceKeys.includes(sourceKey) ? "" : " (현재 없음)"}</option>
          ))}
        </PolicySelect>
        <PolicySelect label="영상" value={output.videoMode} disabled={disabled} onChange={(value) => onChange({ videoMode: value as StreamOutputSettings["videoMode"], ...(value === "copy" ? { maxWidth: null, maxHeight: null, maxFPS: null } : {}) })}>
          <option value="auto">자동</option>
          <option value="copy">원본 복사</option>
          <option value="h264">H.264 변환</option>
        </PolicySelect>
        <PolicySelect label="오디오" value={output.audioMode} disabled={disabled} onChange={(value) => onChange({ audioMode: value as StreamOutputSettings["audioMode"] })}>
          <option value="source">원본</option>
          <option value="none">없음</option>
          <option value="aac">AAC</option>
        </PolicySelect>
        <PolicySelect label="실행" value={output.activation} disabled={disabled} onChange={(value) => onChange({ activation: value as StreamOutputSettings["activation"] })}>
          <option value="on_demand">요청 시 실행</option>
          <option value="always">항상 준비</option>
        </PolicySelect>
      </div>

      <div className="new-policy-limits">
        <label className="new-policy-toggle">
          <input
            type="checkbox"
            checked={limitedSize}
            disabled={disabled || output.videoMode === "copy"}
            onChange={(event) => onChange(event.target.checked ? { maxWidth: 1920, maxHeight: 1080 } : { maxWidth: null, maxHeight: null })}
          />
          <span>최대 해상도 제한</span>
        </label>
        <label>
          <span>폭</span>
          <input className="new-form-control" type="number" min={2} step={2} disabled={disabled || !limitedSize || output.videoMode === "copy"} value={output.maxWidth ?? ""} onChange={(event) => onChange({ maxWidth: nullableNumber(event.target.value) })} />
        </label>
        <label>
          <span>높이</span>
          <input className="new-form-control" type="number" min={2} step={2} disabled={disabled || !limitedSize || output.videoMode === "copy"} value={output.maxHeight ?? ""} onChange={(event) => onChange({ maxHeight: nullableNumber(event.target.value) })} />
        </label>
        <label>
          <span>최대 FPS</span>
          <input className="new-form-control" type="number" min={1} max={60} step={1} placeholder="원본 유지" disabled={disabled || output.videoMode === "copy"} value={output.maxFPS ?? ""} onChange={(event) => onChange({ maxFPS: nullableNumber(event.target.value) })} />
        </label>
      </div>

      {status && (
        <div className="new-policy-facts">
          <PolicyFact label="원본 프로파일" value={status.source.label || status.sourceKey} />
          <PolicyFact label="광고 입력" value={descriptor(status.source.advertised)} />
          <PolicyFact label="실제 입력" value={descriptor(status.source.detected)} />
          <PolicyFact label="요청 정책" value={policySummary(output)} />
          <PolicyFact label="적용 정책" value={applied ? policySummary(applied) : "미적용"} />
          <PolicyFact label="실제 출력" value={effective ? `${descriptor(effective)} · ${effective.transcoding ? "software H.264 변환" : "원본 전달"}` : "미검증"} />
          <PolicyFact label="런타임" value={`${runtimeLabel(status.runtime.state)} · producer ${status.runtime.producerCount} / consumer ${status.runtime.consumerCount} / viewer ${status.runtime.viewerCount}`} />
          <PolicyFact label="검사 시각" value={status.verification.checkedAt || status.source.checkedAt || "-"} />
          {(status.source.error || status.verification.error) && <div className="new-policy-error">{status.source.error || status.verification.error}</div>}
        </div>
      )}
    </section>
  );
}

function PolicySelect({ label, value, disabled, onChange, children }: { label: string; value: string; disabled: boolean; onChange: (value: string) => void; children: React.ReactNode }) {
  return (
    <label>
      <span>{label}</span>
      <select className="new-form-control" value={value} disabled={disabled} onChange={(event) => onChange(event.target.value)}>{children}</select>
    </label>
  );
}

function PolicyFact({ label, value }: { label: string; value: string }) {
  return <div><span>{label}</span><strong>{value}</strong></div>;
}

function nullableNumber(value: string): number | null {
  if (value === "") return null;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : null;
}

function sourceOptions(available: readonly CameraSourceKey[], selected: CameraSourceKey): CameraSourceKey[] {
  return [...new Set([...available, selected])];
}

function descriptor(value: { videoCodec?: string; audioCodec?: string; width?: number; height?: number; fps?: number } | null): string {
  if (!value) return "-";
  return [value.videoCodec, value.width && value.height ? `${value.width}×${value.height}` : "", value.fps ? `${value.fps}fps` : "", value.audioCodec].filter(Boolean).join(" · ") || "-";
}

function policySummary(value: StreamOutputSettings): string {
  const size = value.maxWidth && value.maxHeight ? `${value.maxWidth}×${value.maxHeight}` : "원본 크기";
  return `${value.videoMode} · ${size} · ${value.maxFPS ? `${value.maxFPS}fps` : "원본 FPS"} · ${value.audioMode}`;
}

function verificationLabel(state?: CameraStreamOutput["verification"]["state"]): string {
  if (state === "healthy") return "정상";
  if (state === "degraded") return "저하";
  return "미검증";
}

function runtimeLabel(state: CameraStreamOutput["runtime"]["state"]): string {
  if (state === "running") return "실행 중";
  if (state === "starting") return "시작 중";
  return "대기";
}
