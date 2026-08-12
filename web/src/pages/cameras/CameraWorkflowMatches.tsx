import type { CameraProfileMatch } from "../../app/api";

type MatchListProps = {
  matches: readonly CameraProfileMatch[];
  selectedTemplateId?: number;
  templates: readonly {
    id: number;
    profileName: string;
  }[];
  onSelect: (templateId: number | undefined) => void;
};

export function MatchList({ matches, selectedTemplateId, templates, onSelect }: MatchListProps) {
  if (matches.length === 0) {
    return <div className="new-empty-inline">일치하는 프로파일 템플릿이 없습니다.</div>;
  }
  return (
    <div className="new-stream-list" aria-label="프로파일 매칭">
      {matches.map((match) => {
        const template = templates.find((item) => item.id === match.templateId);
        return (
          <button className="new-stream-row" type="button" key={match.templateId} aria-pressed={selectedTemplateId === match.templateId} onClick={() => onSelect(match.templateId)}>
            <div>
              <strong>{template?.profileName ?? match.name}</strong>
              <span>{match.reasons.join(" · ") || "매칭 근거 없음"}</span>
            </div>
            <em>{match.confidence}%</em>
          </button>
        );
      })}
    </div>
  );
}
