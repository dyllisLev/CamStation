import { Eye, EyeOff } from "lucide-react";
import type { CameraFormState } from "./model";

type ConnectionFieldsProps = {
  form: CameraFormState;
  showPassword: boolean;
  streamNameLocked: boolean;
  onFieldChange: <K extends keyof CameraFormState>(field: K, value: CameraFormState[K]) => void;
  onTogglePassword: () => void;
};

export function ConnectionFields({ form, showPassword, streamNameLocked, onFieldChange, onTogglePassword }: ConnectionFieldsProps) {
  return (
    <>
      <div className="new-form-row">
        <label>
          <span>카메라 이름</span>
          <input className="new-form-control" value={form.name} onChange={(event) => onFieldChange("name", event.target.value)} />
        </label>
        <label>
          <span>내부 키</span>
          <input className="new-form-control" value={form.streamName} onChange={(event) => onFieldChange("streamName", event.target.value)} disabled={streamNameLocked} readOnly={streamNameLocked} />
        </label>
      </div>
      <label>
        <span>호스트</span>
        <input className="new-form-control" value={form.host} onChange={(event) => onFieldChange("host", event.target.value)} required />
      </label>
      <div className="new-form-row">
        <label>
          <span>계정</span>
          <input className="new-form-control" value={form.username} onChange={(event) => onFieldChange("username", event.target.value)} autoComplete="username" />
        </label>
        <div className="new-field">
          <span>비밀번호</span>
          <div className="new-secret-field">
            <input aria-label="비밀번호" className="new-form-control" value={form.password} onChange={(event) => onFieldChange("password", event.target.value)} type={showPassword ? "text" : "password"} autoComplete="current-password" />
            <button type="button" onClick={onTogglePassword} aria-label={showPassword ? "입력값 숨기기" : "입력값 보기"}>
              {showPassword ? <EyeOff size={14} /> : <Eye size={14} />}
            </button>
          </div>
        </div>
      </div>
      <div className="new-form-row new-form-row-ports">
        <label>
          <span>RTSP</span>
          <input className="new-form-control" value={form.rtspPort} onChange={(event) => onFieldChange("rtspPort", event.target.value)} inputMode="numeric" />
        </label>
        <label>
          <span>HTTP</span>
          <input className="new-form-control" value={form.httpPort} onChange={(event) => onFieldChange("httpPort", event.target.value)} inputMode="numeric" />
        </label>
        <label>
          <span>ONVIF</span>
          <input className="new-form-control" value={form.onvifPort} onChange={(event) => onFieldChange("onvifPort", event.target.value)} inputMode="numeric" />
        </label>
      </div>
      <label>
        <span>어댑터</span>
        <select className="new-form-control" value={form.adapter} onChange={(event) => onFieldChange("adapter", event.target.value)}>
          <option value="auto">자동 감지</option>
          <option value="reolink">Reolink Clear 메인</option>
          <option value="vstarcam">VStarcam</option>
          <option value="onvif">ONVIF 일반</option>
        </select>
      </label>
    </>
  );
}
