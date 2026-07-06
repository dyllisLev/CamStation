import type {
	CameraProfileTemplate,
	CameraProfileTemplateChannel,
	CameraProfileTemplateInput,
	DeviceProfile,
} from "../../app/api";
import { selectedCandidate, type RoleSelection } from "./model";
import { ProfileLibraryValidationError } from "./profileLibraryErrors";
import { channelsFromMappingText, mappingLine, streamLine } from "./profileLibraryMapping";

export type ProfileTemplateDraftSource = {
  readonly profile: DeviceProfile;
  readonly selection: RoleSelection;
};

export type ProfileLibraryFormState = {
  readonly profileName: string;
  readonly manufacturer: string;
  readonly model: string;
  readonly adapter: string;
  readonly version: string;
  readonly mappingText: string;
  readonly onvif: boolean;
  readonly rtsp: boolean;
  readonly snapshot: boolean;
  readonly multiChannel: boolean;
};

const defaultMappingText = "0|main|recording|main|manual|/main|PROFILE_MAIN|h264|1920|1080|15|2048\n0|main|live|sub|manual|/sub|PROFILE_SUB|h264|640|360|15|512";

export const emptyProfileForm: ProfileLibraryFormState = {
  profileName: "",
  manufacturer: "",
  model: "",
  adapter: "manual",
  version: "1",
  mappingText: defaultMappingText,
  onvif: false,
  rtsp: true,
  snapshot: false,
  multiChannel: false,
};

export function formFromTemplate(template: CameraProfileTemplate): ProfileLibraryFormState {
  return {
    profileName: template.profileName,
    manufacturer: template.manufacturer,
    model: template.model,
    adapter: template.adapter,
    version: String(template.version),
    mappingText: mappingTextFromChannels(template.channels),
    onvif: template.capabilities.onvif ?? false,
    rtsp: template.capabilities.rtsp ?? false,
    snapshot: template.capabilities.snapshot ?? false,
    multiChannel: template.capabilities.multiChannel ?? template.channels.length > 1,
  };
}

export function formFromDraftSource(source: ProfileTemplateDraftSource): ProfileLibraryFormState {
  const recording = selectedCandidate(source.profile, source.selection.channelIndex, source.selection.recordingProfileToken);
  const live = selectedCandidate(source.profile, source.selection.channelIndex, source.selection.liveProfileToken);
  const selectedChannel = source.profile.channels.find((channel) => channel.index === source.selection.channelIndex);
  const channelName = selectedChannel?.label ?? `channel ${source.selection.channelIndex}`;
  const streams = [mappingLine(source.selection.channelIndex, channelName, recording), mappingLine(source.selection.channelIndex, channelName, live)]
    .filter((line) => line.length > 0)
    .join("\n");
  return {
    profileName: `${source.profile.manufacturer} ${source.profile.model}`.trim(),
    manufacturer: source.profile.manufacturer,
    model: source.profile.model,
    adapter: source.profile.adapter || "manual",
    version: "1",
    mappingText: streams || defaultMappingText,
    onvif: source.profile.adapter === "onvif",
    rtsp: true,
    snapshot: source.profile.capabilities.siren || false,
    multiChannel: source.profile.channels.length > 1,
  };
}

export function profileInputFromForm(form: ProfileLibraryFormState): CameraProfileTemplateInput {
  const profileName = form.profileName.trim();
  const manufacturer = form.manufacturer.trim();
  const model = form.model.trim();
  const adapter = form.adapter.trim();
  const version = Number.parseInt(form.version, 10);
  if (!profileName || !manufacturer || !model || !adapter || !Number.isFinite(version) || version < 1) {
    throw new ProfileLibraryValidationError("프로파일명, 제조사, 모델, 어댑터, 버전을 확인하세요.");
  }
  const channels = channelsFromMappingText(form.mappingText);
  return {
    profileName,
    manufacturer,
    model,
    adapter,
    version,
    matchRules: [
      { field: "manufacturer", operator: "equals", value: manufacturer },
      { field: "model", operator: "contains", value: model },
    ],
    channels,
    capabilities: {
      onvif: form.onvif,
      rtsp: form.rtsp,
      snapshot: form.snapshot,
      multiChannel: form.multiChannel || channels.length > 1,
    },
  };
}

export function mappingTextFromChannels(channels: readonly CameraProfileTemplateChannel[]): string {
  return channels
    .flatMap((channel) => channel.streams.map((stream) => streamLine(channel, stream)))
    .join("\n");
}

export function profileTemplateSummary(template: CameraProfileTemplate): string {
  const streamCount = template.channels.reduce((total, channel) => total + channel.streams.length, 0);
  return `${template.channels.length}채널 · ${streamCount}스트림`;
}
