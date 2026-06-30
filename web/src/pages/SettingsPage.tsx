import { Bell, Clock, Database, HardDrive, KeyRound, SlidersHorizontal, Video } from "lucide-react";
import type { Language } from "../app/language";
import { useLanguage } from "../app/useLanguage";
import { FeatureMatrix } from "../components/FeatureMatrix";
import { Panel, PanelBody, PanelHeader } from "../components/ui/panel";

export function SettingsPage() {
  const { language, setLanguage, t } = useLanguage();

  return (
    <div className="space-y-4">
      <Panel>
        <PanelHeader>
          <h2 className="text-sm font-semibold">{t("languageSettings")}</h2>
        </PanelHeader>
        <PanelBody className="space-y-3">
          <label className="block space-y-2">
            <span className="text-xs font-medium text-slate-400">{t("language")}</span>
            <select
              className="h-10 w-full max-w-xs rounded-md border border-slate-800 bg-slate-950 px-3 text-sm outline-none focus:border-sky-500"
              value={language}
              onChange={(event) => setLanguage(event.target.value as Language)}
            >
              <option value="ko">{t("korean")}</option>
              <option value="en">{t("english")}</option>
            </select>
          </label>
          <p className="text-sm text-slate-500">{t("applyImmediately")}</p>
        </PanelBody>
      </Panel>

      <FeatureMatrix
        title={t("nvrSettings")}
        items={[
          { icon: Video, title: t("segmentLength"), status: t("unknown"), detail: t("segmentLengthDetail") },
          { icon: Clock, title: t("retentionDays"), status: t("unknown"), detail: t("retentionDaysDetail") },
          { icon: HardDrive, title: t("maxStorage"), status: t("unknown"), detail: t("maxStorageDetail") },
          { icon: Bell, title: t("alertRules"), status: t("unknown"), detail: t("alertRulesDetail") },
          { icon: KeyRound, title: t("secrets"), status: t("unknown"), detail: t("secretsDetail") },
          { icon: Database, title: t("importExport"), status: t("unknown"), detail: t("importExportDetail") },
          { icon: SlidersHorizontal, title: t("profiles"), status: t("unknown"), detail: t("profilesDetail") },
        ]}
      />
    </div>
  );
}
