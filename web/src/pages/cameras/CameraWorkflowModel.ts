import type { Camera, CreateCamera, DeviceProfile, StreamOutputSettingsTuple } from "../../app/api";
import { streamSelections, toScanRequest, type CameraFormState, type RoleSelection } from "./model";

export type WorkflowMode = "create" | "edit";

export function formFromCamera(camera: Camera): CameraFormState {
  return {
    name: camera.name,
    streamName: camera.streamName,
    host: camera.host ?? "",
    username: "admin",
    password: "",
    rtspPort: String(camera.rtspPort ?? ""),
    httpPort: String(camera.httpPort ?? ""),
    onvifPort: String(camera.onvifPort ?? ""),
    adapter: camera.profileAdapter || "auto",
  };
}

export function cameraPayload(
  mode: WorkflowMode,
  form: CameraFormState,
  scan: DeviceProfile,
  selection: RoleSelection,
  selectedTemplateId: number | undefined,
  camera: Camera | null,
  streamOutputs?: StreamOutputSettingsTuple,
): CreateCamera {
  const streamName = mode === "edit" && camera ? camera.streamName : form.streamName.trim();
  return {
    ...toScanRequest(form),
    name: form.name.trim() || camera?.name || "카메라",
    ...(streamName ? { streamName } : {}),
    ...(selectedTemplateId !== undefined ? { profileTemplateId: selectedTemplateId } : {}),
    profile: scan,
    channelIndex: selection.channelIndex,
    streamSelections: streamSelections(selection),
    ...(streamOutputs ? { streamOutputs } : {}),
  };
}
