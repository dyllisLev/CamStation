import { useMemo, useState } from "react";
import type { Camera, DeviceProfile } from "../app/api";
import { useCameras } from "../app/queries";
import { Button } from "../components/ui/button";
import { CameraWorkflow } from "./cameras/CameraWorkflow";
import { CameraSummary } from "./cameras/CameraSummary";
import { CameraStreamPolicyEditor } from "./cameras/CameraStreamPolicyEditor";
import { ProfileLibrary } from "./cameras/ProfileLibrary";
import type { ProfileTemplateDraftSource } from "./cameras/profileLibraryModel";
import { RegisteredCameraTable } from "./cameras/RegisteredCameraTable";
import { cameraPolicySurfaceKey } from "./cameras/streamOutputPolicyModel";

type CameraWorkflowMode = "create" | "edit";

export function CamerasPage() {
  const cameras = useCameras();
  const rows = useMemo(() => cameras.data ?? [], [cameras.data]);
  const [lastProfile, setLastProfile] = useState<DeviceProfile | null>(null);
  const [profileDraftSource, setProfileDraftSource] = useState<ProfileTemplateDraftSource | null>(null);
  const [selectedStreamName, setSelectedStreamName] = useState<string | null>(null);
  const [workflowMode, setWorkflowMode] = useState<CameraWorkflowMode>("edit");
  const selectedCamera = useMemo<Camera | null>(() => {
    if (rows.length === 0) return null;
    return rows.find((camera) => camera.streamName === selectedStreamName) ?? rows[0];
  }, [rows, selectedStreamName]);
  const activeMode: CameraWorkflowMode = workflowMode === "edit" && selectedCamera ? "edit" : "create";

  function selectCamera(streamName: string) {
    setSelectedStreamName(streamName);
    setWorkflowMode("edit");
  }

  return (
    <div className="new-camera-admin">
      <CameraSummary cameras={rows} profile={lastProfile} />
      <RegisteredCameraTable
        cameras={rows}
        selectedStreamName={selectedCamera?.streamName ?? null}
        onSelectCamera={selectCamera}
      />
      <div className="new-camera-actions">
        <Button type="button" variant={activeMode === "create" ? "primary" : "secondary"} onClick={() => setWorkflowMode("create")}>
          카메라 등록
        </Button>
        {selectedCamera && (
          <Button type="button" variant={activeMode === "edit" ? "primary" : "secondary"} onClick={() => setWorkflowMode("edit")}>
            카메라 수정
          </Button>
        )}
      </div>
      <CameraWorkflow
        key={cameraPolicySurfaceKey(activeMode, selectedCamera?.streamName)}
        mode={activeMode}
        camera={activeMode === "edit" ? selectedCamera : null}
        onScanComplete={setLastProfile}
        onProfileDraftChange={setProfileDraftSource}
        onDeleted={() => {
          setSelectedStreamName(null);
          setWorkflowMode("create");
        }}
      />
      {activeMode === "edit" && selectedCamera && (
        <CameraStreamPolicyEditor key={cameraPolicySurfaceKey("edit", selectedCamera.streamName)} camera={selectedCamera} />
      )}
      <ProfileLibrary draftSource={profileDraftSource} />
    </div>
  );
}
