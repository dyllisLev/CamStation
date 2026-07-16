import { Loader2, RefreshCw, Send, Trash2 } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import type { ViewerCommand } from "../../app/api";
import {
  useCancelViewerCommand,
  useCreateViewerCommand,
  useDeleteViewerCommand,
  useViewerCommands,
  useViewers,
} from "../../app/streamsViewersSystemQueries";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { canCancelViewerCommand, commandBadgeState, displayViewer, errorMessage, formatDate } from "./viewerFormat";

type Props = {
  readonly selectedViewerId: string;
  readonly onSelectViewer: (id: string) => void;
};

type ConfirmState = {
  readonly action: "cancel" | "delete";
  readonly commandId: number;
} | null;

export function ViewerCommandPanel({ selectedViewerId, onSelectViewer }: Props) {
  const viewers = useViewers();
  const [viewerId, setViewerId] = useState(selectedViewerId);
  const commands = useViewerCommands(viewerId);
  const createCommand = useCreateViewerCommand();
  const cancelCommand = useCancelViewerCommand();
  const deleteCommand = useDeleteViewerCommand();
  const [type, setType] = useState("ping");
  const [message, setMessage] = useState("");
  const [route, setRoute] = useState("/live?viewer=1");
  const [streamName, setStreamName] = useState("");
  const [confirm, setConfirm] = useState<ConfirmState>(null);
  const viewerOptions = viewers.data ?? [];
  const commandRows = commands.data ?? [];
  const commandsLoading = viewers.isLoading || (viewerId !== "" && (commands.isLoading || (commands.data === undefined && !commands.error)));

  useEffect(() => {
    setViewerId(selectedViewerId);
  }, [selectedViewerId]);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    createCommand.mutate({ id: viewerId, command: { type, message, route, streamName } });
  }

  function runConfirmed(action: "cancel" | "delete", command: ViewerCommand) {
    if (confirm?.action !== action || confirm.commandId !== command.id) {
      setConfirm({ action, commandId: command.id });
      return;
    }
    if (action === "cancel") {
      cancelCommand.mutate({ id: command.viewerId, commandID: command.id, reason: "operator cancelled" }, { onSettled: () => setConfirm(null) });
      return;
    }
    deleteCommand.mutate({ id: command.viewerId, commandID: command.id }, { onSettled: () => setConfirm(null) });
  }

  return (
    <Panel>
      <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">Viewer 명령</h2>
          <div className="mt-1 text-xs text-slate-500">Agent 전달과 실행 결과를 독립적으로 추적합니다.</div>
        </div>
        <Button disabled={!viewerId} size="sm" type="button" variant="secondary" onClick={() => void commands.refetch()}>
          <RefreshCw size={15} />
          새로고침
        </Button>
      </PanelHeader>
      <PanelBody className="space-y-4">
        <form className="grid gap-3 lg:grid-cols-[1fr_12rem_1fr_1fr_1fr_auto]" onSubmit={submit}>
          <label className="block space-y-2">
            <span className="text-xs font-medium text-slate-400">대상 Viewer</span>
            <input
              className="new-form-control font-mono"
              list="viewer-command-targets"
              value={viewerId}
              onChange={(event) => {
                setViewerId(event.target.value);
                onSelectViewer(event.target.value);
              }}
            />
            <datalist id="viewer-command-targets">
              {viewerOptions.map((viewer) => (
                <option key={viewer.id} value={viewer.id}>{displayViewer(viewer)}</option>
              ))}
            </datalist>
          </label>
          <label className="block space-y-2">
            <span className="text-xs font-medium text-slate-400">명령</span>
            <select className="new-form-control" value={type} onChange={(event) => setType(event.target.value)}>
              <option value="ping">ping</option>
              <option value="reload_live">reload_live</option>
              <option value="restart_viewer">restart_viewer</option>
              <option value="restart_agent">restart_agent</option>
              <option value="resubscribe_stream">resubscribe_stream</option>
              <option value="restart_stream">restart_stream</option>
              <option value="capture_diagnostics">capture_diagnostics</option>
            </select>
          </label>
          <Field label="스트림" value={streamName} onChange={setStreamName} />
          <Field label="경로" value={route} onChange={setRoute} />
          <Field label="메시지" value={message} onChange={setMessage} />
          <Button className="self-end" disabled={!viewerId || createCommand.isPending} type="submit" variant="primary">
            {createCommand.isPending ? <Loader2 className="animate-spin" size={16} /> : <Send size={16} />}
            명령 보내기
          </Button>
        </form>

        <div className="new-table-wrap">
          <table className="new-table">
            <thead>
              <tr>
                <th className="px-3 py-2 font-medium">ID</th>
                <th className="px-3 py-2 font-medium">명령</th>
                <th className="px-3 py-2 font-medium">상태</th>
                <th className="px-3 py-2 font-medium">생성</th>
                <th className="px-3 py-2 font-medium">작업</th>
              </tr>
            </thead>
            <tbody>
              {commandRows.map((command) => (
                <tr key={command.id}>
                  <td className="whitespace-nowrap px-3 py-3 font-mono text-xs text-slate-300">#{command.id}</td>
                  <td className="px-3 py-3">
                    <div className="text-slate-200">{command.type}</div>
                    <div className="mt-1 text-xs text-slate-500">
                      {command.streamName || command.desiredVersion || command.message || command.route || "-"}
                    </div>
                  </td>
                  <td className="px-3 py-3"><Badge value={commandBadgeState(command.state)} /></td>
                  <td className="whitespace-nowrap px-3 py-3 text-slate-500">
                    <div>{formatDate(command.createdAt)}</div>
                    <div className="mt-1 text-[11px] text-slate-600">
                      전달 {formatDate(command.deliveredAt)} · 결과 {formatDate(command.resultAt)}
                    </div>
                  </td>
                  <td className="px-3 py-3">
                    <CommandActions
                      command={command}
                      confirm={confirm}
                      pending={cancelCommand.isPending || deleteCommand.isPending}
                      onConfirmed={runConfirmed}
                    />
                  </td>
                </tr>
              ))}
              {commandsLoading && (
                <tr>
                  <td className="px-3 py-8 text-center text-sm text-slate-500" colSpan={5}>
                    <div className="flex items-center justify-center gap-2">
                      <Loader2 className="animate-spin text-cyan-300" size={16} />
                      Viewer 명령을 불러오는 중입니다.
                    </div>
                  </td>
                </tr>
              )}
              {!commandsLoading && commandRows.length === 0 && (
                <tr>
                  <td className="px-3 py-8 text-center text-sm text-slate-500" colSpan={5}>
                    선택한 Viewer 명령이 없습니다.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        {commands.error && <div className="text-xs text-red-300">{errorMessage(commands.error)}</div>}
        {createCommand.error && <div className="text-xs text-red-300">{errorMessage(createCommand.error)}</div>}
        {cancelCommand.error && <div className="text-xs text-red-300">{errorMessage(cancelCommand.error)}</div>}
        {deleteCommand.error && <div className="text-xs text-red-300">{errorMessage(deleteCommand.error)}</div>}
      </PanelBody>
    </Panel>
  );
}

function CommandActions({
  command,
  confirm,
  pending,
  onConfirmed,
}: {
  readonly command: ViewerCommand;
  readonly confirm: ConfirmState;
  readonly pending: boolean;
  readonly onConfirmed: (action: "cancel" | "delete", command: ViewerCommand) => void;
}) {
  const deletable = ["pending", "delivered", "cancelled"].includes(command.state);
  return (
    <div className="flex flex-wrap gap-2">
      <Button disabled={pending || !canCancelViewerCommand(command.state)} size="sm" type="button" variant="danger" onClick={() => onConfirmed("cancel", command)}>
        {confirm?.action === "cancel" && confirm.commandId === command.id ? "취소 확인" : "취소"}
      </Button>
      <Button disabled={pending || !deletable} size="sm" type="button" variant="danger" onClick={() => onConfirmed("delete", command)}>
        <Trash2 size={14} />
        {confirm?.action === "delete" && confirm.commandId === command.id ? "삭제 확인" : "삭제"}
      </Button>
    </div>
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
