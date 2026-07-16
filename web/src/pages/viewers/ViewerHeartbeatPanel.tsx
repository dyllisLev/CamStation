import { HeartPulse, Loader2 } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useViewerHeartbeat } from "../../app/streamsViewersSystemQueries";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { errorMessage } from "./viewerFormat";

type HeartbeatDraft = {
  readonly id: string;
  readonly displayName: string;
  readonly appVersion: string;
  readonly hostname: string;
  readonly deviceLabel: string;
  readonly route: string;
  readonly mode: string;
};

const initialDraft: HeartbeatDraft = {
  id: "viewer-qa-01",
  displayName: "QA Viewer",
  appVersion: "2.0.0",
  hostname: "viewer-host",
  deviceLabel: "control-room",
  route: "/live",
  mode: "grid",
};

export function ViewerHeartbeatPanel({ onRegistered }: { readonly onRegistered: (id: string) => void }) {
  const heartbeat = useViewerHeartbeat();
  const [draft, setDraft] = useState(initialDraft);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    heartbeat.mutate({ ...draft, streams: [] }, { onSuccess: (response) => onRegistered(response.viewer.id) });
  }

  return (
    <Panel>
      <PanelHeader>
        <h2 className="text-sm font-semibold">하트비트 등록</h2>
      </PanelHeader>
      <PanelBody>
        <form className="space-y-3" onSubmit={submit}>
          <Field label="Viewer ID" value={draft.id} onChange={(id) => setDraft((current) => ({ ...current, id }))} />
          <Field label="표시명" value={draft.displayName} onChange={(displayName) => setDraft((current) => ({ ...current, displayName }))} />
          <Field label="버전" value={draft.appVersion} onChange={(appVersion) => setDraft((current) => ({ ...current, appVersion }))} />
          <Field label="호스트" value={draft.hostname} onChange={(hostname) => setDraft((current) => ({ ...current, hostname }))} />
          <Field label="장치 라벨" value={draft.deviceLabel} onChange={(deviceLabel) => setDraft((current) => ({ ...current, deviceLabel }))} />
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="경로" value={draft.route} onChange={(route) => setDraft((current) => ({ ...current, route }))} />
            <Field label="모드" value={draft.mode} onChange={(mode) => setDraft((current) => ({ ...current, mode }))} />
          </div>
          <Button className="w-full" disabled={heartbeat.isPending} type="submit" variant="primary">
            {heartbeat.isPending ? <Loader2 className="animate-spin" size={16} /> : <HeartPulse size={16} />}
            하트비트 전송
          </Button>
          {heartbeat.error && <div className="text-xs text-red-300">{errorMessage(heartbeat.error)}</div>}
          {heartbeat.isSuccess && <div className="text-xs text-emerald-300">Viewer 상태가 등록되었습니다.</div>}
        </form>
      </PanelBody>
    </Panel>
  );
}

function Field({
  label,
  value,
  onChange,
}: {
  readonly label: string;
  readonly value: string;
  readonly onChange: (value: string) => void;
}) {
  return (
    <label className="block space-y-2">
      <span className="text-xs font-medium text-slate-400">{label}</span>
      <input className="new-form-control" value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}
