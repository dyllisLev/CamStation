import { Download, MonitorDown } from "lucide-react";
import { withAppBase } from "../../app/basePath";
import { useViewerRelease } from "../../app/queries";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { formatReleaseSize, viewerDownloadHref } from "./viewerReleaseModel";

export function ViewerClientDownloadCard() {
  const release = useViewerRelease();
  const fixedDownloadRoute = release.data ? viewerDownloadHref(release.data.downloadUrl) : null;

  return (
    <Panel>
      <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <MonitorDown size={16} className="text-cyan-300" />
          <h2 className="text-sm font-semibold">Windows 모니터링 클라이언트</h2>
        </div>
        {release.data?.developmentUnsigned && <Badge value="개발용 미서명 빌드" />}
      </PanelHeader>
      <PanelBody>
        {release.isLoading ? (
          <div className="text-sm text-slate-400">설치 파일 정보를 불러오는 중입니다.</div>
        ) : release.data && fixedDownloadRoute ? (
          <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
            <div className="grid gap-3 sm:grid-cols-3">
              <ReleaseMetric label="버전" value={release.data.version} />
              <ReleaseMetric label="파일 크기" value={formatReleaseSize(release.data.sizeBytes)} />
              <ReleaseMetric label="SHA-256" value={release.data.sha256.slice(0, 12)} mono />
            </div>
            <Button asChild variant="primary">
              <a href={withAppBase(fixedDownloadRoute)} download={release.data.filename}>
                <Download size={16} />
                Windows 설치 파일 다운로드
              </a>
            </Button>
          </div>
        ) : (
          <div className="text-sm text-slate-400">설치 파일이 아직 게시되지 않았습니다.</div>
        )}
      </PanelBody>
    </Panel>
  );
}

function ReleaseMetric({ label, value, mono = false }: { readonly label: string; readonly value: string; readonly mono?: boolean }) {
  return (
    <div className="new-feature-card min-w-0">
      <div className="text-xs text-slate-500">{label}</div>
      <div className={`mt-1 truncate text-sm font-semibold text-slate-100 ${mono ? "font-mono" : ""}`}>{value}</div>
    </div>
  );
}
