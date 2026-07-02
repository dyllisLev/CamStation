import { useMemo, useState } from "react";
import type { Camera, DeviceProfile } from "../app/api";
import { useCameras } from "../app/queries";
import { CameraProfileRegistration } from "./cameras/CameraProfileRegistration";
import { CameraSummary } from "./cameras/CameraSummary";
import { RegisteredCameraProfile } from "./cameras/RegisteredCameraProfile";
import { RegisteredCameraTable } from "./cameras/RegisteredCameraTable";

export function CamerasPage() {
  const cameras = useCameras();
  const rows = useMemo(() => cameras.data ?? [], [cameras.data]);
  const [lastProfile, setLastProfile] = useState<DeviceProfile | null>(null);
  const [selectedCameraId, setSelectedCameraId] = useState<number | null>(null);
  const selectedCamera = useMemo<Camera | null>(() => {
    if (rows.length === 0) return null;
    return rows.find((camera) => camera.id === selectedCameraId) ?? rows[0];
  }, [rows, selectedCameraId]);

  return (
    <div className="new-camera-admin">
      <CameraSummary cameras={rows} profile={lastProfile} />
      <CameraProfileRegistration onProfileScanned={setLastProfile} />
      <RegisteredCameraTable
        cameras={rows}
        selectedCameraId={selectedCamera?.id ?? null}
        onSelectCamera={setSelectedCameraId}
      />
      <RegisteredCameraProfile camera={selectedCamera} />
    </div>
  );
}
