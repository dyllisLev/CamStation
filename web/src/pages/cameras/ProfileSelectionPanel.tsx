import {
  Loader2,
  Mic2,
  Play,
  RadioTower,
  ScanSearch,
  Siren,
  Speaker,
  X,
  type LucideIcon,
} from "lucide-react";
import type { DeviceProfile, StreamCandidate } from "../../app/api";
import { useMseStream } from "../../components/live/useMseStream";
import { Button } from "../../components/ui/button";
import { cn } from "../../lib/utils";
import { MutationError } from "./Feedback";
import {
  candidateLabel,
  candidatesForChannel,
  defaultRoleSelection,
  formatSize,
  roleLabel,
  selectedCandidate,
  type PreviewTarget,
  type RoleSelection,
} from "./model";

export function ProfileSelectionPanel({
  profile,
  selection,
  preview,
  previewPending,
  previewError,
  onSelectionChange,
  onPreview,
  onClosePreview,
}: {
  profile: DeviceProfile;
  selection: RoleSelection;
  preview: PreviewTarget | null;
  previewPending: boolean;
  previewError?: string;
  onSelectionChange: (selection: RoleSelection) => void;
  onPreview: (role: "recording" | "live", profileToken: string) => void;
  onClosePreview: () => void;
}) {
  const candidates = candidatesForChannel(profile, selection.channelIndex);
  const recording = selectedCandidate(profile, selection.channelIndex, selection.recordingProfileToken);
  const live = selectedCandidate(profile, selection.channelIndex, selection.liveProfileToken);

  function updateChannel(channelIndex: number) {
    const channelProfile = profile.channels.find((channel) => channel.index === channelIndex);
    if (!channelProfile) return;
    onSelectionChange(defaultRoleSelection({ ...profile, channels: [channelProfile] }));
  }

  return (
    <div className="new-profile-preview">
      <ProfileIdentity profile={profile} />
      <div className="new-capability-strip">
        <Capability enabled={profile.capabilities.ptz} label={`PTZ${profile.capabilities.maxPresets ? ` ${profile.capabilities.maxPresets}` : ""}`} icon={ScanSearch} />
        <Capability enabled={profile.capabilities.audio} label="오디오" icon={RadioTower} />
        <Capability enabled={profile.capabilities.microphone} label="마이크" icon={Mic2} />
        <Capability enabled={profile.capabilities.speaker} label="스피커" icon={Speaker} />
        <Capability enabled={profile.capabilities.siren} label="사이렌" icon={Siren} />
      </div>
      <div className="new-role-selector">
        <label>
          <span>채널</span>
          <select className="new-form-control" value={selection.channelIndex} onChange={(event) => updateChannel(Number(event.target.value))}>
            {profile.channels.map((channel) => (
              <option key={channel.index} value={channel.index}>
                {channel.label || `CH ${channel.index}`}
              </option>
            ))}
          </select>
        </label>
        <RoleSelect
          label="녹화 스트림"
          value={selection.recordingProfileToken}
          candidates={candidates}
          onChange={(profileToken) => onSelectionChange({ ...selection, recordingProfileToken: profileToken })}
        />
        <RoleSelect
          label="라이브 스트림"
          value={selection.liveProfileToken}
          candidates={candidates}
          onChange={(profileToken) => onSelectionChange({ ...selection, liveProfileToken: profileToken })}
        />
      </div>
      <div className="new-role-actions">
        <Button type="button" variant="secondary" disabled={!recording?.profileToken || previewPending} onClick={() => onPreview("recording", selection.recordingProfileToken)}>
          {previewPending ? <Loader2 className="animate-spin" size={15} /> : <Play size={15} />}
          녹화 미리보기
        </Button>
        <Button type="button" variant="secondary" disabled={!live?.profileToken || previewPending} onClick={() => onPreview("live", selection.liveProfileToken)}>
          {previewPending ? <Loader2 className="animate-spin" size={15} /> : <Play size={15} />}
          라이브 미리보기
        </Button>
      </div>
      {preview && (
        <div className="new-inline-preview">
          <div className="new-inline-preview-head">
            <div>
              <span>미리보기</span>
              <strong>{preview.label}</strong>
            </div>
            <button className="new-icon-button" type="button" onClick={onClosePreview} aria-label="미리보기 닫기">
              <X size={15} />
            </button>
          </div>
          <ProfilePreviewPlayer streamName={preview.streamName} />
        </div>
      )}
      <MutationError message={previewError} />
      <div className="new-stream-list" aria-label="역할별 스트림">
        {candidates.map((candidate, index) => (
          <StreamCandidateRow candidate={candidate} key={`${candidate.profileToken ?? candidate.label}-${index}`} />
        ))}
      </div>
    </div>
  );
}

function ProfileIdentity({ profile }: { profile: DeviceProfile }) {
  return (
    <div className="new-profile-identity">
      <div>
        <span>제조사</span>
        <strong>{profile.manufacturer}</strong>
      </div>
      <div>
        <span>모델</span>
        <strong>{profile.model}</strong>
      </div>
      <div>
        <span>어댑터</span>
        <strong>{profile.adapter}</strong>
      </div>
    </div>
  );
}

function RoleSelect({ label, value, candidates, onChange }: { label: string; value: string; candidates: StreamCandidate[]; onChange: (value: string) => void }) {
  return (
    <label>
      <span>{label}</span>
      <select className="new-form-control" value={value} onChange={(event) => onChange(event.target.value)}>
        {candidates.map((candidate, index) => (
          <option key={`${candidate.profileToken ?? candidate.label}-${index}`} value={candidate.profileToken ?? ""}>
            {candidateLabel(candidate)}
          </option>
        ))}
      </select>
    </label>
  );
}

function Capability({ enabled, label, icon: Icon }: { enabled: boolean; label: string; icon: LucideIcon }) {
  return (
    <span className={cn("new-capability", enabled && "new-enabled")}>
      <Icon size={13} />
      {label}
    </span>
  );
}

function StreamCandidateRow({ candidate }: { candidate: StreamCandidate }) {
  return (
    <div className="new-stream-row">
      <div>
        <strong>{roleLabel(candidate.roleHint)} · {candidate.label}</strong>
        <span>{candidate.redactedUrl ?? "-"}</span>
      </div>
      <em>{candidate.codec || "codec"} {formatSize(candidate.width, candidate.height)} {candidate.fps ? `${candidate.fps}fps` : ""}</em>
    </div>
  );
}

function ProfilePreviewPlayer({ streamName }: { streamName: string }) {
  const { videoRef, connected } = useMseStream(streamName);
  return (
    <div className="new-profile-preview-player">
      <video ref={videoRef} className="new-live-video" autoPlay muted playsInline disablePictureInPicture controls={false} />
      {!connected && <div className="new-offline-layer">연결 중...</div>}
    </div>
  );
}
