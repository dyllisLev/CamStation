import { useQuery } from "@tanstack/react-query";
import { api } from "./api";

export const viewerReleaseKeys = {
  current: ["viewer-release"] as const,
};

export function useViewerRelease() {
  return useQuery({ queryKey: viewerReleaseKeys.current, queryFn: api.viewerRelease });
}
