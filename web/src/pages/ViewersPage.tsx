import { useState } from "react";
import { ViewerCommandPanel } from "./viewers/ViewerCommandPanel";
import { ViewerHeartbeatPanel } from "./viewers/ViewerHeartbeatPanel";
import { ViewerRegistryPanel } from "./viewers/ViewerRegistryPanel";

export function ViewersPage() {
  const [selectedViewerId, setSelectedViewerId] = useState("");

  return (
    <div className="space-y-4">
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
        <ViewerRegistryPanel selectedViewerId={selectedViewerId} onSelectViewer={setSelectedViewerId} />
        <ViewerHeartbeatPanel onRegistered={setSelectedViewerId} />
      </div>
      <ViewerCommandPanel selectedViewerId={selectedViewerId} onSelectViewer={setSelectedViewerId} />
    </div>
  );
}
