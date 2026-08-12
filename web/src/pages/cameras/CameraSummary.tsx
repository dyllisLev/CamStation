import { RadioTower, ScanSearch, ShieldCheck, Video, type LucideIcon } from "lucide-react";
import type { Camera, DeviceProfile } from "../../app/api";

export function CameraSummary({ cameras, profile }: { cameras: Camera[]; profile: DeviceProfile | null }) {
  const online = cameras.filter((camera) => camera.state === "streaming").length;
  const vstarcam = cameras.filter((camera) => camera.profileAdapter === "vstarcam").length;
  const roleStreams = cameras.reduce((count, camera) => count + (camera.streams?.length ?? 0), 0);

  return (
    <section className="new-control-summary" aria-label="카메라 등록 요약">
      <SummaryStat label="등록 카메라" value={`${online}/${cameras.length}`} detail="온라인 / 전체" icon={Video} />
      <SummaryStat label="VStarcam" value={String(vstarcam)} detail="감지된 프로파일" icon={ShieldCheck} />
      <SummaryStat label="역할별 스트림" value={String(roleStreams)} detail="녹화 / 라이브 후보" icon={RadioTower} />
      <SummaryStat label="최근 스캔" value={profile?.adapter ?? "-"} detail={profile ? `${profile.manufacturer} ${profile.model}` : "대기 중"} icon={ScanSearch} />
    </section>
  );
}

function SummaryStat({ label, value, detail, icon: Icon }: { label: string; value: string; detail: string; icon: LucideIcon }) {
  return (
    <div className="new-control-stat">
      <div className="new-feature-icon"><Icon size={17} /></div>
      <div>
        <div className="new-control-stat-label">{label}</div>
        <div className="new-control-stat-value">{value}</div>
        <div className="new-control-stat-detail">{detail}</div>
      </div>
    </div>
  );
}
