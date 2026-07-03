import { Bell, CheckCircle2, Loader2, RotateCcw, Siren, TimerReset, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type { Incident, IncidentInput, IncidentQuery } from "../../app/api";
import {
  useAcknowledgeIncident,
  useCreateIncident,
  useDeleteIncident,
  useIncident,
  useIncidents,
  useResolveIncident,
  useSnoozeIncident,
  useUpdateIncident,
} from "../../app/queries";
import { useLanguage } from "../../app/useLanguage";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { formatDate } from "../../lib/utils";
import { incidentLabels } from "./incidentLabels";

const severities = ["low", "medium", "high", "critical"] as const;
const statuses = ["open", "acknowledged", "snoozed", "resolved"] as const;

type FormState = {
  readonly title: string;
  readonly source: string;
  readonly severity: string;
  readonly description: string;
};

const initialForm: FormState = { title: "", source: "manual", severity: "high", description: "" };

export function IncidentConsole() {
  const { language } = useLanguage();
  const labels = incidentLabels[language];
  const [status, setStatus] = useState("");
  const [severity, setSeverity] = useState("");
  const [source, setSource] = useState("");
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [form, setForm] = useState<FormState>(initialForm);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [armedDeleteId, setArmedDeleteId] = useState<number | null>(null);
  const query = useMemo<IncidentQuery>(
    () => ({
      status: emptyToUndefined(status),
      severity: emptyToUndefined(severity),
      source: emptyToUndefined(source),
      limit: 100,
    }),
    [severity, source, status],
  );
  const incidents = useIncidents(query);
  const selected = useIncident(selectedId ?? 0);
  const createIncident = useCreateIncident();
  const updateIncident = useUpdateIncident();
  const acknowledgeIncident = useAcknowledgeIncident();
  const snoozeIncident = useSnoozeIncident();
  const resolveIncident = useResolveIncident();
  const deleteIncident = useDeleteIncident();
  const rows = incidents.data?.incidents ?? [];
  const firstIncidentId = rows[0]?.id;
  const activeIncident = selected.data ?? rows.find((incident) => incident.id === selectedId) ?? rows[0];

  useEffect(() => {
    if (!selectedId && firstIncidentId) {
      setSelectedId(firstIncidentId);
    }
  }, [firstIncidentId, selectedId]);

  async function createManualIncident() {
    setError("");
    setNotice("");
    const input: IncidentInput = {
      title: form.title.trim(),
      source: form.source.trim() || "manual",
      severity: form.severity,
      description: emptyToUndefined(form.description.trim()),
    };
    try {
      const created = await createIncident.mutateAsync(input);
      setSelectedId(created.id);
      setForm(initialForm);
      setNotice(labels.successCreate);
    } catch (caught) {
      setError(errorMessage(caught, labels.errorPrefix));
    }
  }

  async function runAction(action: "ack" | "snooze" | "resolve" | "reopen", incident: Incident) {
    setError("");
    setNotice("");
    setArmedDeleteId(null);
    try {
      if (action === "ack") {
        await acknowledgeIncident.mutateAsync(incident.id);
      } else if (action === "snooze") {
        await snoozeIncident.mutateAsync({ id: incident.id, input: { until: oneHourFromNow() } });
      } else if (action === "resolve") {
        await resolveIncident.mutateAsync(incident.id);
      } else {
        await updateIncident.mutateAsync({ id: incident.id, incident: { status: "open" } });
      }
      setNotice(labels.successAction);
    } catch (caught) {
      setArmedDeleteId(null);
      setError(errorMessage(caught, labels.errorPrefix));
    }
  }

  async function deleteSelected(incident: Incident) {
    setError("");
    setNotice("");
    if (armedDeleteId !== incident.id) {
      setArmedDeleteId(incident.id);
      return;
    }
    try {
      await deleteIncident.mutateAsync(incident.id);
      setArmedDeleteId(null);
      setSelectedId(null);
      setNotice(labels.successDelete);
    } catch (caught) {
      setError(errorMessage(caught, labels.errorPrefix));
    }
  }

  return (
    <div className="space-y-4" data-testid="incidents-console">
      <Panel>
        <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="text-sm font-semibold">{labels.title}</h2>
            <div className="mt-1 text-xs text-slate-500">{labels.subtitle}</div>
          </div>
          <Button size="sm" variant="secondary" onClick={() => void incidents.refetch()}>
            {incidents.isFetching ? <Loader2 className="animate-spin" size={15} /> : <RotateCcw size={15} />}
            {labels.loading}
          </Button>
        </PanelHeader>
        <PanelBody className="grid gap-3 md:grid-cols-4">
          <Select label={labels.status} value={status} onChange={setStatus} values={statuses} labels={labels.statusLabels} allLabel={labels.all} />
          <Select label={labels.severity} value={severity} onChange={setSeverity} values={severities} labels={labels.severityLabels} allLabel={labels.all} />
          <Field label={labels.source} value={source} onChange={setSource} placeholder="manual" />
          <div className="new-feature-card text-xs text-slate-400">{labels.deleteOpenHint}</div>
        </PanelBody>
      </Panel>

      <div className="grid gap-4 xl:grid-cols-[20rem_minmax(0,1fr)]">
        <Panel>
          <PanelHeader>
            <h2 className="text-sm font-semibold">{labels.create}</h2>
          </PanelHeader>
          <PanelBody className="space-y-3">
            <Field label={labels.titleField} value={form.title} onChange={(title) => setForm({ ...form, title })} />
            <Field label={labels.source} value={form.source} onChange={(nextSource) => setForm({ ...form, source: nextSource })} />
            <Select label={labels.severity} value={form.severity} onChange={(nextSeverity) => setForm({ ...form, severity: nextSeverity })} values={severities} labels={labels.severityLabels} />
            <label className="block space-y-2">
              <span className="text-xs font-medium text-slate-400">{labels.description}</span>
              <textarea className="new-form-control min-h-24 py-2" value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} />
            </label>
            <Button className="w-full" variant="primary" disabled={createIncident.isPending || !form.title.trim()} onClick={() => void createManualIncident()} data-testid="incident-create">
              {createIncident.isPending ? <Loader2 className="animate-spin" size={16} /> : <Siren size={16} />}
              {labels.save}
            </Button>
          </PanelBody>
        </Panel>

        <Panel>
          <PanelHeader>
            <h2 className="text-sm font-semibold">{labels.list}</h2>
          </PanelHeader>
          <PanelBody className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_24rem]">
            <IncidentList rows={rows} selectedId={activeIncident?.id} loading={incidents.isLoading} empty={labels.empty} onSelect={setSelectedId} />
            <IncidentDetail incident={activeIncident} loading={selected.isLoading} labels={labels} confirming={armedDeleteId === activeIncident?.id} onAction={runAction} onDelete={deleteSelected} />
          </PanelBody>
        </Panel>
      </div>

      {notice && <div className="rounded-[7px] border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-sm text-emerald-200">{notice}</div>}
      {error && <div className="rounded-[7px] border border-red-500/35 bg-red-500/10 px-3 py-2 text-sm text-red-200" data-testid="incident-error">{error}</div>}
    </div>
  );
}

function IncidentList({ rows, selectedId, loading, empty, onSelect }: { readonly rows: readonly Incident[]; readonly selectedId?: number; readonly loading: boolean; readonly empty: string; readonly onSelect: (id: number) => void }) {
  if (loading) return <div className="py-10 text-center text-sm text-slate-500">Loading</div>;
  if (rows.length === 0) return <div className="py-10 text-center text-sm text-slate-500">{empty}</div>;
  return (
    <div className="space-y-2">
      {rows.map((incident) => (
        <button key={incident.id} type="button" className={`new-feature-card block w-full text-left ${selectedId === incident.id ? "border-cyan-500/60" : ""}`} onClick={() => onSelect(incident.id)}>
          <div className="flex items-center justify-between gap-3">
            <span className="truncate text-sm font-semibold text-slate-100">{incident.title}</span>
            <Badge value={incident.status} />
          </div>
          <div className="mt-2 flex items-center gap-2 text-xs text-slate-500">
            <span>{incident.source}</span>
            <span>{formatDate(incident.updatedAt)}</span>
          </div>
        </button>
      ))}
    </div>
  );
}

function IncidentDetail({ incident, loading, labels, confirming, onAction, onDelete }: { readonly incident?: Incident; readonly loading: boolean; readonly labels: (typeof incidentLabels)["ko"]; readonly confirming: boolean; readonly onAction: (action: "ack" | "snooze" | "resolve" | "reopen", incident: Incident) => void; readonly onDelete: (incident: Incident) => void }) {
  if (loading) return <div className="py-10 text-center text-sm text-slate-500">{labels.loading}</div>;
  if (!incident) return <div className="py-10 text-center text-sm text-slate-500">{labels.empty}</div>;
  return (
    <div className="new-feature-card space-y-3" data-testid="incident-detail">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="text-sm font-semibold text-slate-100">{incident.title}</div>
          <div className="mt-1 text-xs text-slate-500">{labels.updated}: {formatDate(incident.updatedAt)}</div>
        </div>
        <div className="flex gap-2"><Badge value={incident.severity} /><Badge value={incident.status} /></div>
      </div>
      <div className="grid gap-2 text-xs text-slate-400 md:grid-cols-2">
        <Info label={labels.source} value={incident.source} />
        <Info label="ID" value={String(incident.id)} />
        <Info label="ack" value={formatDate(incident.acknowledgedAt)} />
        <Info label="snooze" value={formatDate(incident.snoozedUntil)} />
      </div>
      {incident.description && <p className="text-sm text-slate-300">{incident.description}</p>}
      {incident.details && <pre className="max-h-40 overflow-auto rounded-[7px] border border-slate-800 bg-slate-950 p-2 text-xs text-slate-400">{JSON.stringify(incident.details, null, 2)}</pre>}
      <div className="flex flex-wrap gap-2">
        <Button size="sm" onClick={() => onAction("ack", incident)}><CheckCircle2 size={15} />{labels.ack}</Button>
        <Button size="sm" onClick={() => onAction("snooze", incident)}><TimerReset size={15} />{labels.snooze}</Button>
        <Button size="sm" onClick={() => onAction("resolve", incident)}><Bell size={15} />{labels.resolve}</Button>
        <Button size="sm" onClick={() => onAction("reopen", incident)}><RotateCcw size={15} />{labels.reopen}</Button>
        <Button size="sm" variant="danger" onClick={() => onDelete(incident)} data-testid="incident-delete"><Trash2 size={15} />{confirming ? labels.confirmDelete : labels.remove}</Button>
      </div>
    </div>
  );
}

function Field({ label, value, onChange, placeholder }: { readonly label: string; readonly value: string; readonly onChange: (value: string) => void; readonly placeholder?: string }) {
  return <label className="block space-y-2"><span className="text-xs font-medium text-slate-400">{label}</span><input className="new-form-control" value={value} placeholder={placeholder} onChange={(event) => onChange(event.target.value)} /></label>;
}

function Select({ label, value, values, labels, allLabel, onChange }: { readonly label: string; readonly value: string; readonly values: readonly string[]; readonly labels: Record<string, string>; readonly allLabel?: string; readonly onChange: (value: string) => void }) {
  return <label className="block space-y-2"><span className="text-xs font-medium text-slate-400">{label}</span><select className="new-form-control" value={value} onChange={(event) => onChange(event.target.value)}>{allLabel && <option value="">{allLabel}</option>}{values.map((item) => <option key={item} value={item}>{labels[item] ?? item}</option>)}</select></label>;
}

function Info({ label, value }: { readonly label: string; readonly value: string }) {
  return <div className="grid grid-cols-[5rem_minmax(0,1fr)] gap-3"><span className="text-slate-500">{label}</span><span className="truncate text-right font-mono">{value}</span></div>;
}

function oneHourFromNow() {
  return new Date(Date.now() + 60 * 60 * 1000).toISOString();
}

function emptyToUndefined(value: string) {
  return value ? value : undefined;
}

function errorMessage(error: unknown, prefix: string) {
  return error instanceof Error ? `${prefix}: ${error.message}` : prefix;
}
