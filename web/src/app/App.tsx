import { Navigate, Route, Routes } from "react-router-dom";
import { ConsoleLayout } from "../layouts/ConsoleLayout";
import { BackupPage } from "../pages/BackupPage";
import { CamerasPage } from "../pages/CamerasPage";
import { ControlRoomPage } from "../pages/ControlRoomPage";
import { IncidentsPage } from "../pages/IncidentsPage";
import { LivePage } from "../pages/LivePage";
import { LogsPage } from "../pages/LogsPage";
import { RecordingsPage } from "../pages/RecordingsPage";
import { SettingsPage } from "../pages/SettingsPage";
import { StreamsPage } from "../pages/StreamsPage";
import { SystemPage } from "../pages/SystemPage";
import { ViewersPage } from "../pages/ViewersPage";

export function App() {
  return (
    <Routes>
      <Route element={<ConsoleLayout />}>
        <Route index element={<ControlRoomPage />} />
        <Route path="live" element={<LivePage />} />
        <Route path="recordings" element={<RecordingsPage />} />
        <Route path="cameras" element={<CamerasPage />} />
        <Route path="incidents" element={<IncidentsPage />} />
        <Route path="streams" element={<StreamsPage />} />
        <Route path="backup" element={<BackupPage />} />
        <Route path="viewers" element={<ViewersPage />} />
        <Route path="logs" element={<LogsPage />} />
        <Route path="system" element={<SystemPage />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}
