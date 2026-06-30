import type { ComponentType } from "react";
import { Badge } from "./ui/badge";
import { Panel, PanelBody, PanelHeader } from "./ui/panel";

type FeatureItem = {
  icon: ComponentType<{ size?: number }>;
  title: string;
  status: string;
  detail: string;
};

export function FeatureMatrix({ title, items }: { title: string; items: FeatureItem[] }) {
  return (
    <Panel>
      <PanelHeader>
        <h2 className="text-sm font-semibold">{title}</h2>
      </PanelHeader>
      <PanelBody>
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {items.map((item) => (
            <div key={item.title} className="rounded-md border border-slate-800 bg-slate-950 p-3">
              <div className="flex items-center justify-between gap-3">
                <div className="flex items-center gap-2">
                  <div className="flex size-8 items-center justify-center rounded-md bg-slate-900 text-sky-300">
                    <item.icon size={16} />
                  </div>
                  <div className="text-sm font-medium">{item.title}</div>
                </div>
                <Badge value={item.status} />
              </div>
              <div className="mt-3 text-sm text-slate-400">{item.detail}</div>
            </div>
          ))}
        </div>
      </PanelBody>
    </Panel>
  );
}

