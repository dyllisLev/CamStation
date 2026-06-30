import { Bell, BellOff, CheckCircle2, Siren, TimerReset } from "lucide-react";
import { useEvents } from "../app/queries";
import { Badge } from "../components/ui/badge";
import { Panel, PanelBody, PanelHeader } from "../components/ui/panel";
import { FeatureMatrix } from "../components/FeatureMatrix";
import { formatDate } from "../lib/utils";

export function IncidentsPage() {
  const events = useEvents();
  const errors = (events.data ?? []).filter((event) => event.level === "error");

  return (
    <div className="space-y-4">
      <FeatureMatrix
        title="Incident Controls"
        items={[
          { icon: Siren, title: "Open Incidents", status: errors.length ? "error" : "info", detail: `${errors.length} active error events` },
          { icon: Bell, title: "Alert Webhook", status: "unknown", detail: "Webhook delivery state is not wired in this prototype." },
          { icon: TimerReset, title: "Cooldown", status: "unknown", detail: "Incident dampening rules will replace event spam." },
          { icon: BellOff, title: "Snooze", status: "unknown", detail: "Snooze and acknowledge controls are reserved for incidents." },
        ]}
      />
      <Panel>
        <PanelHeader>
          <h2 className="text-sm font-semibold">Error Events</h2>
        </PanelHeader>
        <PanelBody className="space-y-3">
          {errors.map((event) => (
            <div key={event.id} className="flex items-start justify-between gap-3 rounded-md border border-slate-800 p-3">
              <div>
                <div className="flex items-center gap-2">
                  <CheckCircle2 size={15} className="text-red-300" />
                  <span className="text-sm font-medium">{event.message}</span>
                </div>
                <div className="mt-1 text-xs text-slate-500">
                  {event.source} · {formatDate(event.createdAt)}
                </div>
              </div>
              <Badge value={event.level} />
            </div>
          ))}
          {errors.length === 0 && <div className="py-10 text-center text-sm text-slate-500">No error events.</div>}
        </PanelBody>
      </Panel>
    </div>
  );
}

