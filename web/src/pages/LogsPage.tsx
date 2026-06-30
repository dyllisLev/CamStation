import { useMemo, useState } from "react";
import { useEvents } from "../app/queries";
import { Badge } from "../components/ui/badge";
import { Panel, PanelBody, PanelHeader } from "../components/ui/panel";
import { formatDate } from "../lib/utils";

export function LogsPage() {
  const events = useEvents();
  const [level, setLevel] = useState("all");
  const [source, setSource] = useState("all");
  const rows = useMemo(() => events.data ?? [], [events.data]);
  const sources = useMemo(() => Array.from(new Set(rows.map((event) => event.source))), [rows]);
  const filtered = rows.filter((event) => {
    if (level !== "all" && event.level !== level) return false;
    if (source !== "all" && event.source !== source) return false;
    return true;
  });

  return (
    <Panel>
      <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-sm font-semibold">Events</h2>
        <div className="flex flex-wrap gap-2">
          <select
            className="new-form-control h-9 w-auto"
            value={level}
            onChange={(event) => setLevel(event.target.value)}
          >
            <option value="all">All levels</option>
            <option value="info">Info</option>
            <option value="warning">Warning</option>
            <option value="error">Error</option>
          </select>
          <select
            className="new-form-control h-9 w-auto"
            value={source}
            onChange={(event) => setSource(event.target.value)}
          >
            <option value="all">All sources</option>
            {sources.map((item) => (
              <option key={item} value={item}>
                {item}
              </option>
            ))}
          </select>
        </div>
      </PanelHeader>
      <PanelBody>
        <div className="new-table-wrap">
          <table className="new-table">
            <thead>
              <tr>
                <th className="px-3 py-2 font-medium">Time</th>
                <th className="px-3 py-2 font-medium">Level</th>
                <th className="px-3 py-2 font-medium">Source</th>
                <th className="px-3 py-2 font-medium">Message</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((event) => (
                <tr key={event.id}>
                  <td className="whitespace-nowrap px-3 py-3 text-slate-500">{formatDate(event.createdAt)}</td>
                  <td className="px-3 py-3">
                    <Badge value={event.level} />
                  </td>
                  <td className="whitespace-nowrap px-3 py-3 text-slate-400">{event.source}</td>
                  <td className="px-3 py-3">
                    <div className="text-slate-200">{event.message}</div>
                    {event.details && (
                      <details className="mt-2 text-xs text-slate-500">
                        <summary className="cursor-pointer">details</summary>
                        <pre className="mt-2 overflow-auto rounded-md bg-slate-950 p-2">
                          {JSON.stringify(event.details, null, 2)}
                        </pre>
                      </details>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </PanelBody>
    </Panel>
  );
}
