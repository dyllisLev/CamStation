import { request } from "./http";

export type ViewerRelease = {
  readonly version: string;
  readonly filename: string;
  readonly sizeBytes: number;
  readonly sha256: string;
  readonly publishedAt: string;
  readonly developmentUnsigned: boolean;
  readonly downloadUrl: "/api/viewers/app/download";
};

export const viewerReleaseApi = {
  viewerRelease: () => request<ViewerRelease>("/api/viewers/app/version"),
} as const;
