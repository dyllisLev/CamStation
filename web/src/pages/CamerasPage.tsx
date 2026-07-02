import { useMemo, useState } from "react";
import type { DeviceProfile } from "../app/api";
import { useCameras } from "../app/queries";
import { CameraProfileRegistration } from "./cameras/CameraProfileRegistration";
import { CameraSummary } from "./cameras/CameraSummary";
import { RegisteredCameraTable } from "./cameras/RegisteredCameraTable";

export function CamerasPage() {
  const cameras = useCameras();
  const rows = useMemo(() => cameras.data ?? [], [cameras.data]);
  const [lastProfile, setLastProfile] = useState<DeviceProfile | null>(null);

  return (
    <div className="new-camera-admin">
      <CameraSummary cameras={rows} profile={lastProfile} />
      <CameraProfileRegistration onProfileScanned={setLastProfile} />
      <RegisteredCameraTable cameras={rows} />
    </div>
  );
}
