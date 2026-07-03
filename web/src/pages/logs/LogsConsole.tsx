import { Download, Loader2, RotateCcw, ScissorsLineDashed } from "lucide-react";
import { useMemo, useState } from "react";
import type { EventLog, EventQuery } from "../../app/api";
import { useEventPage, useExportEvents, usePruneEvents } from "../../app/queries";
import { useLanguage } from "../../app/useLanguage";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { formatDate } from "../../lib/utils";
import { logLabels, type LogLabels, type LogLevel } from "./logLabels";

const levels: readonly LogLevel[] = ["info", "warning", "error"];

type FilterState = {
  readonly level: string;
  readonly source: string;
  readonly search: string;
  readonly from: string;
  readonly to: string;
  readonly limit: string;
  readonly cursor: string;
};

const initialFilters: FilterState = { level: "", source: "", search: "", from: "", to: "", limit: "50", cursor: "" };

export function LogsConsole() {
  const { language } = useLanguage();
  const labels = logLabels[language];
  const [filters, setFilters] = useState<FilterState>(initialFilters);
  const [cursorStack, setCursorStack] = useState<readonly string[]>([]);
  const [pruneBefore, setPruneBefore] = useState("");
  const [armedPrune, setArmedPrune] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const query = useMemo<EventQuery>(
    () => ({
      level: emptyToUndefined(filters.level),
      source: emptyToUndefined(filters.source.trim()),
      search: emptyToUndefined(filters.search.trim()),
      from: localToIso(filters.from),
      to: localToIso(filters.to),
      cursor: emptyToUndefined(filters.cursor),
      limit: positiveNumber(filters.limit) ?? 50,
    }),
    [filters],
  );
  const eventPage = useEventPage(query);
  const exportEvents = useExportEvents();
  const pruneEvents = usePruneEvents();
  const rows = eventPage.data?.events ?? [];

  async function exportCurrentQuery() {
    setError("");
    setNotice("");
    try {
      const result = await exportEvents.mutateAsync({ ...query, format: "json" });
      downloadJson(result.events, "camstation-events.json");
      setNotice(labels.successExport);
    } catch (caught) {
      setError(caught instanceof Error ? `${labels.errorPrefix}: ${caught.message}` : labels.errorPrefix);
    }
  }

  async function pruneLogs() {
    setError("");
    setNotice("");
    if (!armedPrune) {
      setArmedPrune(true);
      return;
    }
    try {
      const result = await pruneEvents.mutateAsync({
        confirm: true,
        before: localToIso(pruneBefore),
        level: emptyToUndefined(filters.level),
        source: emptyToUndefined(filters.source.trim()),
        search: emptyToUndefined(filters.search.trim()),
      });
      setArmedPrune(false);
      setNotice(`${labels.successPrune} (${result.deleted})`);
    } catch (caught) {
      setError(caught instanceof Error ? `${labels.errorPrefix}: ${caught.message}` : labels.errorPrefix);
    }
  }

  function updateFilter(field: keyof FilterState, value: string) {
    setFilters((current) => ({ ...current, [field]: value, cursor: field === "cursor" ? value : "" }));
    if (field !== "cursor") {
      setCursorStack([]);
    }
  }

  function nextPage() {
    const nextCursor = eventPage.data?.nextCursor;
    if (!nextCursor) return;
    setCursorStack((current) => [...current, filters.cursor]);
    setFilters((current) => ({ ...current, cursor: nextCursor }));
  }

  function previousPage() {
    const previous = cursorStack[cursorStack.length - 1];
    setCursorStack((current) => current.slice(0, -1));
    setFilters((current) => ({ ...current, cursor: previous ?? "" }));
  }

  return (
    <div className="space-y-4" data-testid="logs-console">
      <Panel>
        <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="text-sm font-semibold">{labels.title}</h2>
            <div className="mt-1 text-xs text-slate-500">{labels.subtitle}</div>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button size="sm" onClick={() => void eventPage.refetch()}><RotateCcw size={15} />{labels.refresh}</Button>
            <Button size="sm" variant="primary" disabled={exportEvents.isPending} onClick={() => void exportCurrentQuery()} data-testid="logs-export">{exportEvents.isPending ? <Loader2 className="animate-spin" size={15} /> : <Download size={15} />}{labels.export}</Button>
          </div>
        </PanelHeader>
        <PanelBody className="grid gap-3 md:grid-cols-3 xl:grid-cols-6">
          <Select label={labels.level} value={filters.level} values={levels} optionLabels={labels.levelOptions} allLabel={labels.all} onChange={(value) => updateFilter("level", value)} />
          <Field label={labels.source} value={filters.source} onChange={(value) => updateFilter("source", value)} />
          <Field label={labels.search} value={filters.search} onChange={(value) => updateFilter("search", value)} />
          <Field label={labels.from} type="datetime-local" value={filters.from} onChange={(value) => updateFilter("from", value)} />
          <Field label={labels.to} type="datetime-local" value={filters.to} onChange={(value) => updateFilter("to", value)} />
          <Field label={labels.limit} type="number" value={filters.limit} onChange={(value) => updateFilter("limit", value)} />
        </PanelBody>
      </Panel>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_20rem]">
        <Panel>
          <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
            <h2 className="text-sm font-semibold">{labels.events}</h2>
            <div className="flex gap-2">
              <Button size="sm" disabled={cursorStack.length === 0} onClick={previousPage}>{labels.previous}</Button>
              <Button size="sm" disabled={!eventPage.data?.nextCursor} onClick={nextPage}>{labels.next}</Button>
            </div>
          </PanelHeader>
          <PanelBody>
            <LogTable rows={rows} loading={eventPage.isLoading} labels={labels} />
          </PanelBody>
        </Panel>

        <Panel>
          <PanelHeader>
            <h2 className="text-sm font-semibold">{labels.prune}</h2>
          </PanelHeader>
          <PanelBody className="space-y-3">
            <Field label={labels.before} type="datetime-local" value={pruneBefore} onChange={setPruneBefore} />
            <div className="new-feature-card text-xs text-slate-400">{labels.pruneUnsafeHint}</div>
            <Button className="w-full" variant="danger" disabled={pruneEvents.isPending} onClick={() => void pruneLogs()} data-testid="logs-prune">{pruneEvents.isPending ? <Loader2 className="animate-spin" size={15} /> : <ScissorsLineDashed size={15} />}{armedPrune ? labels.confirmPrune : labels.pruneAction}</Button>
          </PanelBody>
        </Panel>
      </div>

      {notice && <div className="rounded-[7px] border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-sm text-emerald-200">{notice}</div>}
      {error && <div className="rounded-[7px] border border-red-500/35 bg-red-500/10 px-3 py-2 text-sm text-red-200" data-testid="logs-error">{error}</div>}
    </div>
  );
}

function LogTable({ rows, loading, labels }: { readonly rows: readonly EventLog[]; readonly loading: boolean; readonly labels: LogLabels }) {
  if (loading) return <div className="py-10 text-center text-sm text-slate-500">{labels.loading}</div>;
  return (
    <>
      <div className="space-y-2 md:hidden">
        {rows.map((event) => (
          <div key={event.id} className="new-feature-card space-y-2">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="break-words text-sm font-semibold text-slate-100">{event.message}</div>
                <div className="mt-1 text-xs text-slate-500">{formatDate(event.createdAt)}</div>
              </div>
              <Badge value={event.level} />
            </div>
            <div className="truncate font-mono text-xs text-slate-400">{event.source}</div>
            {event.details && <details className="text-xs text-slate-500"><summary className="cursor-pointer">{labels.details}</summary><pre className="mt-2 max-h-40 overflow-auto rounded-[7px] border border-slate-800 bg-slate-950 p-2">{JSON.stringify(event.details, null, 2)}</pre></details>}
          </div>
        ))}
        {rows.length === 0 && <div className="py-10 text-center text-sm text-slate-500">{labels.empty}</div>}
      </div>
      <div className="new-table-wrap hidden md:block">
        <table className="new-table">
          <thead><tr><th className="px-3 py-2 font-medium">{labels.time}</th><th className="px-3 py-2 font-medium">{labels.level}</th><th className="px-3 py-2 font-medium">{labels.source}</th><th className="px-3 py-2 font-medium">{labels.message}</th></tr></thead>
          <tbody>
            {rows.map((event) => (
              <tr key={event.id}>
                <td className="whitespace-nowrap px-3 py-3 text-slate-500">{formatDate(event.createdAt)}</td>
                <td className="px-3 py-3"><Badge value={event.level} /></td>
                <td className="whitespace-nowrap px-3 py-3 text-slate-400">{event.source}</td>
                <td className="px-3 py-3"><div className="text-slate-200">{event.message}</div>{event.details && <details className="mt-2 text-xs text-slate-500"><summary className="cursor-pointer">{labels.details}</summary><pre className="mt-2 max-h-40 overflow-auto rounded-[7px] border border-slate-800 bg-slate-950 p-2">{JSON.stringify(event.details, null, 2)}</pre></details>}</td>
              </tr>
            ))}
            {rows.length === 0 && <tr><td className="px-3 py-10 text-center text-sm text-slate-500" colSpan={4}>{labels.empty}</td></tr>}
          </tbody>
        </table>
      </div>
    </>
  );
}

function Field({ label, value, onChange, type = "text" }: { readonly label: string; readonly value: string; readonly onChange: (value: string) => void; readonly type?: string }) {
  return <label className="block space-y-2"><span className="text-xs font-medium text-slate-400">{label}</span><input className="new-form-control" type={type} value={value} onChange={(event) => onChange(event.target.value)} /></label>;
}

function Select({ label, value, values, optionLabels, allLabel, onChange }: { readonly label: string; readonly value: string; readonly values: readonly LogLevel[]; readonly optionLabels: Record<LogLevel, string>; readonly allLabel: string; readonly onChange: (value: string) => void }) {
  return <label className="block space-y-2"><span className="text-xs font-medium text-slate-400">{label}</span><select className="new-form-control" value={value} onChange={(event) => onChange(event.target.value)}><option value="">{allLabel}</option>{values.map((item) => <option key={item} value={item}>{optionLabels[item]}</option>)}</select></label>;
}

function localToIso(value: string) {
  return value ? new Date(value).toISOString() : undefined;
}

function positiveNumber(value: string) {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
}

function emptyToUndefined(value: string) {
  return value ? value : undefined;
}

function downloadJson(events: readonly EventLog[], filename: string) {
  const blob = new Blob([JSON.stringify({ events }, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  URL.revokeObjectURL(url);
}
