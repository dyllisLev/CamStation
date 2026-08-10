import { useState } from "react";
import { ViewerCommandPanel } from "./viewers/ViewerCommandPanel";
import { ViewerRegistryPanel } from "./viewers/ViewerRegistryPanel";

export function ViewersPage() {
  const [selectedViewerId, setSelectedViewerId] = useState("");

  return (
    <div className="space-y-4">
      <ViewerRegistryPanel selectedViewerId={selectedViewerId} onSelectViewer={setSelectedViewerId} />
      <ViewerCommandPanel selectedViewerId={selectedViewerId} onSelectViewer={setSelectedViewerId} />
    </div>
  );
}
