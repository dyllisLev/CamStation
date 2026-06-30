import { Navigate, Route, Routes } from "react-router-dom";
import { ConsoleLayout } from "../layouts/ConsoleLayout";
import { CamerasPage } from "../pages/CamerasPage";
import { DashboardPage } from "../pages/DashboardPage";
import { LivePage } from "../pages/LivePage";
import { LogsPage } from "../pages/LogsPage";
import { RecordingsPage } from "../pages/RecordingsPage";
import { SystemPage } from "../pages/SystemPage";

export function App() {
  return (
    <Routes>
      <Route element={<ConsoleLayout />}>
        <Route index element={<DashboardPage />} />
        <Route path="live" element={<LivePage />} />
        <Route path="cameras" element={<CamerasPage />} />
        <Route path="recordings" element={<RecordingsPage />} />
        <Route path="logs" element={<LogsPage />} />
        <Route path="system" element={<SystemPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}

