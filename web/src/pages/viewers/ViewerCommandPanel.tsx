import { Loader2, RefreshCw, Send, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import type { ViewerCommand, ViewerCommandInput } from "../../app/api";
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
import {
  VIEWER_COMMAND_ACTIONS,
  type OperatorViewerCommand,
  viewerCommandAction,
  viewerCommandErrorLabel,
  viewerCommandTypeLabel,
  viewerCommandUnavailableReason,
} from "./viewerCommandModel";
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
  const commands = useViewerCommands(selectedViewerId);
  const createCommand = useCreateViewerCommand();
  const cancelCommand = useCancelViewerCommand();
  const deleteCommand = useDeleteViewerCommand();
  const [type, setType] = useState<OperatorViewerCommand>("ping");
  const [reason, setReason] = useState("");
  const [streamName, setStreamName] = useState("");
  const [armedAction, setArmedAction] = useState<OperatorViewerCommand | null>(null);
  const [confirm, setConfirm] = useState<ConfirmState>(null);
  const selectedViewer = useMemo(
    () => viewers.data?.find((viewer) => viewer.id === selectedViewerId),
    [selectedViewerId, viewers.data],
  );
  const viewerOptions = viewers.data ?? [];
  const action = viewerCommandAction(type);
  const unavailableReason = viewerCommandUnavailableReason(selectedViewer, action, streamName);
  const commandRows = commands.data ?? [];
  const commandsLoading = viewers.isLoading
    || (selectedViewerId !== "" && (commands.isLoading || (commands.data === undefined && !commands.error)));

  useEffect(() => {
    setArmedAction(null);
    setConfirm(null);
    setStreamName("");
  }, [selectedViewerId]);

  function selectViewer(id: string) {
    setArmedAction(null);
    setConfirm(null);
    onSelectViewer(id);
  }

  function selectAction(nextType: OperatorViewerCommand) {
    setType(nextType);
    setArmedAction(null);
    setReason("");
    if (nextType !== "resubscribe_stream") {
      setStreamName("");
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedViewer || unavailableReason) {
      return;
    }
    if (action.disruptive && armedAction !== action.type) {
      setArmedAction(action.type);
      return;
    }

    const command: ViewerCommandInput = {
      type: action.type,
      ...(action.requiresStream ? { streamName } : {}),
      ...(action.disruptive && reason.trim() ? { message: reason.trim() } : {}),
    };
    createCommand.mutate(
      { id: selectedViewer.id, command },
      {
        onSuccess: () => {
          setArmedAction(null);
          setReason("");
        },
      },
    );
  }

  function runConfirmed(actionName: "cancel" | "delete", command: ViewerCommand) {
    if (confirm?.action !== actionName || confirm.commandId !== command.id) {
      setConfirm({ action: actionName, commandId: command.id });
      return;
    }
    if (actionName === "cancel") {
      cancelCommand.mutate(
        { id: command.viewerId, commandID: command.id, reason: "operator cancelled" },
        { onSettled: () => setConfirm(null) },
      );
      return;
    }
    deleteCommand.mutate(
      { id: command.viewerId, commandID: command.id },
      { onSettled: () => setConfirm(null) },
    );
  }

  return (
    <Panel>
      <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">Viewer 원격 제어</h2>
          <div className="mt-1 text-xs text-slate-500">
            관리 서비스 제어 연결과 Viewer·화면 실행 상태를 분리해 확인하고, 지원되는 작업만 실행합니다.
          </div>
        </div>
        <Button disabled={!selectedViewerId} size="sm" type="button" variant="secondary" onClick={() => void commands.refetch()}>
          <RefreshCw size={15} />
          새로고침
        </Button>
      </PanelHeader>
      <PanelBody className="space-y-4">
        <form className="space-y-3 rounded-[7px] border border-slate-800 bg-slate-950/40 p-3" onSubmit={submit}>
          <div className="grid gap-3 lg:grid-cols-[minmax(14rem,1fr)_minmax(17rem,1fr)_minmax(14rem,1fr)_auto]">
            <label className="block space-y-2">
              <span className="text-xs font-medium text-slate-400">1. 대상 Viewer</span>
              <select
                className="new-form-control"
                value={selectedViewerId}
                onChange={(event) => selectViewer(event.target.value)}
              >
                <option value="">Viewer를 선택하세요</option>
                {viewerOptions.map((viewer) => (
                  <option key={viewer.id} value={viewer.id}>{displayViewer(viewer)}</option>
                ))}
              </select>
            </label>

            <label className="block space-y-2">
              <span className="text-xs font-medium text-slate-400">2. 실행할 기능</span>
              <select
                className="new-form-control"
                value={type}
                onChange={(event) => selectAction(event.target.value as OperatorViewerCommand)}
              >
                {VIEWER_COMMAND_ACTIONS.map((candidate) => (
                  <option key={candidate.type} value={candidate.type}>{candidate.label}</option>
                ))}
              </select>
            </label>

            {action.requiresStream ? (
              <label className="block space-y-2">
                <span className="text-xs font-medium text-slate-400">3. 다시 연결할 카메라</span>
                <select
                  className="new-form-control font-mono"
                  value={streamName}
                  onChange={(event) => {
                    setStreamName(event.target.value);
                    setArmedAction(null);
                  }}
                >
                  <option value="">카메라를 선택하세요</option>
                  {(selectedViewer?.streams ?? []).map((stream) => (
                    <option key={stream.streamName} value={stream.streamName}>{stream.streamName}</option>
                  ))}
                </select>
              </label>
            ) : action.disruptive ? (
              <label className="block space-y-2">
                <span className="text-xs font-medium text-slate-400">3. 작업 사유 (선택)</span>
                <input
                  className="new-form-control"
                  maxLength={256}
                  placeholder="예: 화면 복구"
                  value={reason}
                  onChange={(event) => {
                    setReason(event.target.value);
                    setArmedAction(null);
                  }}
                />
              </label>
            ) : (
              <div className="self-end pb-2 text-xs text-slate-500">추가 입력이 필요하지 않습니다.</div>
            )}

            <Button
              className="self-end"
              disabled={Boolean(unavailableReason) || createCommand.isPending}
              type="submit"
              variant={action.disruptive ? "danger" : "primary"}
            >
              {createCommand.isPending ? <Loader2 className="animate-spin" size={16} /> : <Send size={16} />}
              {action.disruptive && armedAction === action.type ? "실행 확인" : "기능 실행"}
            </Button>
          </div>

          <div className="grid gap-2 text-xs sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
            <div className="text-slate-400">{action.description}</div>
            <div className={unavailableReason ? "text-amber-300" : "text-emerald-300"}>
              {unavailableReason || "현재 상태에서 실행할 수 있습니다."}
            </div>
          </div>
          {action.disruptive && armedAction === action.type && (
            <div className="rounded-[7px] border border-red-500/35 bg-red-500/10 px-3 py-2 text-xs text-red-200">
              {action.label} 작업입니다. 같은 버튼을 한 번 더 눌러 실행하세요.
            </div>
          )}
        </form>

        <div className="new-table-wrap">
          <table className="new-table">
            <thead>
              <tr>
                <th className="px-3 py-2 font-medium">ID</th>
                <th className="px-3 py-2 font-medium">기능</th>
                <th className="px-3 py-2 font-medium">상태</th>
                <th className="px-3 py-2 font-medium">처리 시각</th>
                <th className="px-3 py-2 font-medium">결과</th>
                <th className="px-3 py-2 font-medium">작업</th>
              </tr>
            </thead>
            <tbody>
              {commandRows.map((command) => (
                <tr key={command.id}>
                  <td className="whitespace-nowrap px-3 py-3 font-mono text-xs text-slate-300">#{command.id}</td>
                  <td className="px-3 py-3">
                    <div className="font-medium text-slate-200">{viewerCommandTypeLabel(command.type)}</div>
                    <div className="mt-1 font-mono text-[11px] text-slate-600">{command.type}</div>
                    {(command.streamName || command.message) && (
                      <div className="mt-1 text-xs text-slate-500">{command.streamName || command.message}</div>
                    )}
                  </td>
                  <td className="px-3 py-3"><Badge value={commandBadgeState(command.state)} /></td>
                  <td className="whitespace-nowrap px-3 py-3 text-[11px] text-slate-500">
                    <CommandTime label="생성" value={command.createdAt} />
                    <CommandTime label="전달" value={command.deliveredAt} />
                    <CommandTime label="확인" value={command.acknowledgedAt} />
                    <CommandTime label="실행" value={command.runningAt} />
                    <CommandTime label="결과" value={command.resultAt} />
                  </td>
                  <td className="max-w-72 px-3 py-3 text-xs">
                    {command.error ? (
                      <span className="text-red-300">{viewerCommandErrorLabel(command.error)}</span>
                    ) : command.state === "succeeded" ? (
                      <span className="text-emerald-300">정상 완료되었습니다.</span>
                    ) : (
                      <span className="text-slate-600">-</span>
                    )}
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
                  <td className="px-3 py-8 text-center text-sm text-slate-500" colSpan={6}>
                    <div className="flex items-center justify-center gap-2">
                      <Loader2 className="animate-spin text-cyan-300" size={16} />
                      Viewer 명령을 불러오는 중입니다.
                    </div>
                  </td>
                </tr>
              )}
              {!commandsLoading && commandRows.length === 0 && (
                <tr>
                  <td className="px-3 py-8 text-center text-sm text-slate-500" colSpan={6}>
                    {selectedViewerId ? "선택한 Viewer의 명령 이력이 없습니다." : "먼저 대상 Viewer를 선택하세요."}
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
        {createCommand.isSuccess && createCommand.data && (
          <div className="text-xs text-emerald-300">명령 #{createCommand.data.id}을(를) 등록했습니다. 실행 결과를 자동으로 확인합니다.</div>
        )}
      </PanelBody>
    </Panel>
  );
}

function CommandTime({ label, value }: { readonly label: string; readonly value?: string }) {
  return <div><span className="inline-block w-8 text-slate-600">{label}</span>{formatDate(value)}</div>;
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
  const deletable = command.state === "pending" || command.state === "cancelled";
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
