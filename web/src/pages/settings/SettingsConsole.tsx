import { Bell, DatabaseBackup, KeyRound, Languages, Loader2, RotateCcw, Save, Send, Video } from "lucide-react";
import { useEffect, useState } from "react";
import type { Language } from "../../app/language";
import type { SettingsUpdate } from "../../app/api";
import { useResetSettings, useSettings, useTestAlert, useUpdateSettings } from "../../app/queries";
import { useLanguage } from "../../app/useLanguage";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { settingsLabels } from "./settingsLabels";

type SettingsForm = {
  readonly segmentMinutes: string;
  readonly recordingRetentionDays: string;
  readonly maxStorageGB: string;
  readonly backupEnabled: boolean;
  readonly backupTarget: string;
  readonly backupRetentionDays: string;
  readonly backupScheduleEnabled: boolean;
  readonly backupScheduleIntervalMinutes: string;
  readonly protectUnbacked: boolean;
  readonly discordEnabled: boolean;
  readonly webhook: string;
};

const emptyForm: SettingsForm = {
  segmentMinutes: "5",
  recordingRetentionDays: "14",
  maxStorageGB: "256",
  backupEnabled: false,
  backupTarget: "",
  backupRetentionDays: "14",
  backupScheduleEnabled: false,
  backupScheduleIntervalMinutes: "1440",
  protectUnbacked: true,
  discordEnabled: false,
  webhook: "",
};

export function SettingsConsole() {
  const { language, setLanguage, t } = useLanguage();
  const labels = settingsLabels[language];
  const settings = useSettings();
  const updateSettings = useUpdateSettings();
  const resetSettings = useResetSettings();
  const testAlert = useTestAlert();
  const [form, setForm] = useState<SettingsForm>(emptyForm);
  const [armedReset, setArmedReset] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    const data = settings.data;
    if (!data) return;
    setForm({
      segmentMinutes: String(data.recording.segmentMinutes),
      recordingRetentionDays: String(data.recording.retentionDays),
      maxStorageGB: String(data.recording.maxStorageGB),
      backupEnabled: data.backup.enabled,
      backupTarget: data.backup.target,
      backupRetentionDays: String(data.backup.retentionDays),
      backupScheduleEnabled: data.backup.scheduleEnabled,
      backupScheduleIntervalMinutes: String(data.backup.scheduleIntervalMinutes),
      protectUnbacked: data.backup.protectUnbacked,
      discordEnabled: data.alerts.discordEnabled,
      webhook: "",
    });
  }, [settings.data]);

  async function saveSettings() {
    setNotice("");
    setError("");
    try {
      const update = formToUpdate(form);
      await updateSettings.mutateAsync(update);
      setForm((current) => ({ ...current, webhook: "" }));
      setNotice(labels.successSave);
    } catch (caught) {
      setError(errorMessage(caught, labels.errorPrefix));
    }
  }

  async function resetAllSettings() {
    setNotice("");
    setError("");
    if (!armedReset) {
      setArmedReset(true);
      return;
    }
    try {
      await resetSettings.mutateAsync();
      setArmedReset(false);
      setForm(emptyForm);
      setNotice(labels.successReset);
    } catch (caught) {
      setError(errorMessage(caught, labels.errorPrefix));
    }
  }

  async function sendTestAlert() {
    setNotice("");
    setError("");
    try {
      await testAlert.mutateAsync();
      setNotice(labels.successTest);
    } catch (caught) {
      setError(errorMessage(caught, labels.errorPrefix));
    }
  }

  return (
    <div className="space-y-4" data-testid="settings-console">
      <Panel>
        <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="text-sm font-semibold">{labels.title}</h2>
            <div className="mt-1 text-xs text-slate-500">{labels.subtitle}</div>
          </div>
          {settings.isFetching && <Badge value={labels.loading} />}
        </PanelHeader>
        <PanelBody className="grid gap-3 md:grid-cols-[18rem_minmax(0,1fr)]">
          <label className="block space-y-2">
            <span className="inline-flex items-center gap-2 text-xs font-medium text-slate-400"><Languages size={14} />{labels.language}</span>
            <select className="new-form-control" value={language} onChange={(event) => setLanguage(languageValue(event.target.value))}>
              <option value="ko">{t("korean")}</option>
              <option value="en">{t("english")}</option>
            </select>
          </label>
          <div className="new-feature-card text-xs text-slate-400">{t("applyImmediately")}</div>
        </PanelBody>
      </Panel>

      <div className="grid gap-4 xl:grid-cols-3">
        <Panel>
          <PanelHeader className="flex items-center gap-2"><Video size={16} className="text-cyan-300" /><h2 className="text-sm font-semibold">{labels.recording}</h2></PanelHeader>
          <PanelBody className="space-y-3">
            <Field label={labels.segmentMinutes} type="number" value={form.segmentMinutes} onChange={(value) => setForm({ ...form, segmentMinutes: value })} />
            <Field label={labels.retentionDays} type="number" value={form.recordingRetentionDays} onChange={(value) => setForm({ ...form, recordingRetentionDays: value })} />
            <Field label={labels.maxStorageGB} type="number" value={form.maxStorageGB} onChange={(value) => setForm({ ...form, maxStorageGB: value })} />
          </PanelBody>
        </Panel>

        <Panel>
          <PanelHeader className="flex items-center gap-2"><DatabaseBackup size={16} className="text-cyan-300" /><h2 className="text-sm font-semibold">{labels.backup}</h2></PanelHeader>
          <PanelBody className="space-y-3">
            <Toggle label={labels.enabled} checked={form.backupEnabled} onChange={(checked) => setForm({ ...form, backupEnabled: checked })} />
            <Field label={labels.target} value={form.backupTarget} onChange={(value) => setForm({ ...form, backupTarget: value })} />
            <Toggle label={labels.scheduleEnabled} checked={form.backupScheduleEnabled} onChange={(checked) => setForm({ ...form, backupScheduleEnabled: checked })} />
            <Field label={labels.scheduleIntervalMinutes} type="number" value={form.backupScheduleIntervalMinutes} onChange={(value) => setForm({ ...form, backupScheduleIntervalMinutes: value })} />
            <Toggle label={labels.protectUnbacked} checked={form.protectUnbacked} onChange={(checked) => setForm({ ...form, protectUnbacked: checked })} />
            <Field label={labels.retentionDays} type="number" value={form.backupRetentionDays} onChange={(value) => setForm({ ...form, backupRetentionDays: value })} />
          </PanelBody>
        </Panel>

        <Panel>
          <PanelHeader className="flex items-center gap-2"><Bell size={16} className="text-cyan-300" /><h2 className="text-sm font-semibold">{labels.alerts}</h2></PanelHeader>
          <PanelBody className="space-y-3">
            <Toggle label={labels.discordEnabled} checked={form.discordEnabled} onChange={(checked) => setForm({ ...form, discordEnabled: checked })} />
            <SecretState labels={labels} masked={settings.data?.alerts.discordWebhook.masked} fingerprint={settings.data?.alerts.discordWebhook.fingerprint} hasSecret={settings.data?.alerts.discordWebhook.hasSecret ?? false} />
            <Field label={labels.webhook} type="password" value={form.webhook} onChange={(value) => setForm({ ...form, webhook: value })} autoComplete="new-password" />
            <div className="new-feature-card text-xs text-slate-400">{labels.dryRun}</div>
            <Button className="w-full" disabled={testAlert.isPending} onClick={() => void sendTestAlert()} data-testid="settings-test-alert">{testAlert.isPending ? <Loader2 className="animate-spin" size={15} /> : <Send size={15} />}{labels.testAlert}</Button>
          </PanelBody>
        </Panel>
      </div>

      <div className="flex flex-wrap gap-2">
        <Button variant="primary" disabled={updateSettings.isPending || settings.isLoading} onClick={() => void saveSettings()} data-testid="settings-save">{updateSettings.isPending ? <Loader2 className="animate-spin" size={16} /> : <Save size={16} />}{labels.save}</Button>
        <Button variant="danger" disabled={resetSettings.isPending} onClick={() => void resetAllSettings()} data-testid="settings-reset">{resetSettings.isPending ? <Loader2 className="animate-spin" size={16} /> : <RotateCcw size={16} />}{armedReset ? labels.confirmReset : labels.reset}</Button>
      </div>

      {notice && <div className="rounded-[7px] border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-sm text-emerald-200">{notice}</div>}
      {error && <div className="rounded-[7px] border border-red-500/35 bg-red-500/10 px-3 py-2 text-sm text-red-200" data-testid="settings-error">{error}</div>}
    </div>
  );
}

function Field({ label, value, onChange, type = "text", autoComplete }: { readonly label: string; readonly value: string; readonly onChange: (value: string) => void; readonly type?: string; readonly autoComplete?: string }) {
  return <label className="block space-y-2"><span className="text-xs font-medium text-slate-400">{label}</span><input className="new-form-control" type={type} value={value} autoComplete={autoComplete} onChange={(event) => onChange(event.target.value)} /></label>;
}

function Toggle({ label, checked, onChange }: { readonly label: string; readonly checked: boolean; readonly onChange: (checked: boolean) => void }) {
  return <label className="flex items-center justify-between gap-3 rounded-[7px] border border-slate-800 bg-slate-950/60 px-3 py-2 text-sm text-slate-200"><span>{label}</span><input type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} /></label>;
}

function SecretState({ labels, hasSecret, masked, fingerprint }: { readonly labels: (typeof settingsLabels)["ko"]; readonly hasSecret: boolean; readonly masked?: string; readonly fingerprint?: string }) {
  return (
    <div className="new-feature-card space-y-2" data-testid="settings-secret-state">
      <div className="flex items-center justify-between gap-3">
        <span className="inline-flex items-center gap-2 text-xs text-slate-500"><KeyRound size={14} />{labels.webhookState}</span>
        <span className={`rounded-full border px-2 py-1 text-xs font-semibold ${hasSecret ? "border-emerald-500/40 bg-emerald-500/15 text-emerald-200" : "border-amber-500/40 bg-amber-500/15 text-amber-200"}`}>{hasSecret ? labels.hasSecret : labels.noSecret}</span>
      </div>
      <div className="text-sm font-semibold text-slate-100">{hasSecret ? (masked ?? labels.hasSecret) : labels.noSecret}</div>
      <div className="truncate font-mono text-xs text-slate-500">{labels.fingerprint}: {fingerprint || "-"}</div>
    </div>
  );
}

function formToUpdate(form: SettingsForm): SettingsUpdate {
  const webhook = form.webhook.trim();
  return {
    recording: {
      segmentMinutes: Number(form.segmentMinutes),
      retentionDays: Number(form.recordingRetentionDays),
      maxStorageGB: Number(form.maxStorageGB),
    },
    backup: {
      enabled: form.backupEnabled,
      target: form.backupTarget.trim(),
      retentionDays: Number(form.backupRetentionDays),
      scheduleEnabled: form.backupScheduleEnabled,
      scheduleIntervalMinutes: Number(form.backupScheduleIntervalMinutes),
      protectUnbacked: form.protectUnbacked,
    },
    alerts: {
      discordEnabled: form.discordEnabled,
      webhook: webhook ? webhook : undefined,
    },
  };
}

function languageValue(value: string): Language {
  return value === "en" ? "en" : "ko";
}

function errorMessage(error: unknown, prefix: string) {
  return error instanceof Error ? `${prefix}: ${error.message}` : prefix;
}
