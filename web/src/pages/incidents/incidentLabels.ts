import type { Language } from "../../app/language";

type IncidentLabels = {
  readonly title: string;
  readonly subtitle: string;
  readonly create: string;
  readonly list: string;
  readonly detail: string;
  readonly empty: string;
  readonly loading: string;
  readonly save: string;
  readonly titleField: string;
  readonly source: string;
  readonly severity: string;
  readonly status: string;
  readonly description: string;
  readonly all: string;
  readonly ack: string;
  readonly snooze: string;
  readonly resolve: string;
  readonly reopen: string;
  readonly remove: string;
  readonly confirmDelete: string;
  readonly updated: string;
  readonly deleteOpenHint: string;
  readonly successCreate: string;
  readonly successAction: string;
  readonly successDelete: string;
  readonly errorPrefix: string;
  readonly statusLabels: Record<string, string>;
  readonly severityLabels: Record<string, string>;
};

export const incidentLabels: Record<Language, IncidentLabels> = {
  ko: {
    title: "장애 대응",
    subtitle: "수동 장애를 만들고 확인, 일시중지, 해결, 재개, 삭제합니다.",
    create: "수동 장애 생성",
    list: "장애 목록",
    detail: "장애 상세",
    empty: "조건에 맞는 장애가 없습니다.",
    loading: "불러오는 중",
    save: "생성",
    titleField: "제목",
    source: "출처",
    severity: "심각도",
    status: "상태",
    description: "설명",
    all: "전체",
    ack: "확인",
    snooze: "1시간 일시중지",
    resolve: "해결",
    reopen: "재개",
    remove: "삭제",
    confirmDelete: "다시 눌러 삭제",
    updated: "수정",
    deleteOpenHint: "열린 장애 삭제는 서버가 거부하며 목록은 유지됩니다.",
    successCreate: "장애를 생성했습니다.",
    successAction: "장애 상태를 갱신했습니다.",
    successDelete: "해결된 장애를 삭제했습니다.",
    errorPrefix: "요청 실패",
    statusLabels: {
      open: "열림",
      acknowledged: "확인됨",
      snoozed: "일시중지",
      resolved: "해결됨",
    },
    severityLabels: {
      low: "낮음",
      medium: "보통",
      high: "높음",
      critical: "긴급",
    },
  },
  en: {
    title: "Incident Response",
    subtitle: "Create, acknowledge, snooze, resolve, reopen, and delete incidents.",
    create: "Create Incident",
    list: "Incidents",
    detail: "Incident Detail",
    empty: "No incidents match the filters.",
    loading: "Loading",
    save: "Create",
    titleField: "Title",
    source: "Source",
    severity: "Severity",
    status: "Status",
    description: "Description",
    all: "All",
    ack: "Ack",
    snooze: "Snooze 1h",
    resolve: "Resolve",
    reopen: "Reopen",
    remove: "Delete",
    confirmDelete: "Confirm delete",
    updated: "Updated",
    deleteOpenHint: "Deleting an open incident is rejected by the server and the row remains.",
    successCreate: "Incident created.",
    successAction: "Incident state updated.",
    successDelete: "Resolved incident deleted.",
    errorPrefix: "Request failed",
    statusLabels: {
      open: "Open",
      acknowledged: "Acknowledged",
      snoozed: "Snoozed",
      resolved: "Resolved",
    },
    severityLabels: {
      low: "Low",
      medium: "Medium",
      high: "High",
      critical: "Critical",
    },
  },
};
