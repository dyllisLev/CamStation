import type { Language } from "../../app/language";

export type LogLevel = "info" | "warning" | "error";

export type LogLabels = {
  readonly title: string;
  readonly subtitle: string;
  readonly filters: string;
  readonly events: string;
  readonly prune: string;
  readonly time: string;
  readonly level: string;
  readonly source: string;
  readonly message: string;
  readonly search: string;
  readonly from: string;
  readonly to: string;
  readonly before: string;
  readonly limit: string;
  readonly all: string;
  readonly export: string;
  readonly refresh: string;
  readonly next: string;
  readonly previous: string;
  readonly empty: string;
  readonly confirmPrune: string;
  readonly pruneAction: string;
  readonly pruneUnsafeHint: string;
  readonly successExport: string;
  readonly successPrune: string;
  readonly errorPrefix: string;
  readonly details: string;
  readonly loading: string;
  readonly levelOptions: Record<LogLevel, string>;
};

export const logLabels: Record<Language, LogLabels> = {
  ko: {
    title: "운영 로그",
    subtitle: "서버 필터, 검색, 기간, 커서 페이지, 내보내기, 정리를 한 화면에서 처리합니다.",
    filters: "필터",
    events: "이벤트",
    prune: "로그 정리",
    time: "시간",
    level: "레벨",
    source: "출처",
    message: "메시지",
    search: "검색",
    from: "시작",
    to: "종료",
    before: "삭제 기준 이전",
    limit: "페이지 크기",
    all: "전체",
    export: "JSON 내보내기",
    refresh: "새로고침",
    next: "다음",
    previous: "이전",
    empty: "조건에 맞는 로그가 없습니다.",
    confirmPrune: "다시 눌러 정리",
    pruneAction: "정리 실행",
    pruneUnsafeHint: "정리는 별도 before 값을 서버에 보냅니다. 비워 두면 안전장치 오류가 표시됩니다.",
    successExport: "내보내기 파일을 만들었습니다.",
    successPrune: "로그를 정리했습니다.",
    errorPrefix: "요청 실패",
    details: "상세",
    loading: "불러오는 중",
    levelOptions: {
      info: "정보",
      warning: "경고",
      error: "오류",
    },
  },
  en: {
    title: "Operation Logs",
    subtitle: "Server filters, search, ranges, cursor pages, export, and prune controls.",
    filters: "Filters",
    events: "Events",
    prune: "Prune Logs",
    time: "Time",
    level: "Level",
    source: "Source",
    message: "Message",
    search: "Search",
    from: "From",
    to: "To",
    before: "Delete before",
    limit: "Page size",
    all: "All",
    export: "Export JSON",
    refresh: "Refresh",
    next: "Next",
    previous: "Previous",
    empty: "No logs match the filters.",
    confirmPrune: "Confirm prune",
    pruneAction: "Run prune",
    pruneUnsafeHint: "Prune sends a separate before value. Leaving it blank shows the server safety error.",
    successExport: "Export file created.",
    successPrune: "Logs pruned.",
    errorPrefix: "Request failed",
    details: "Details",
    loading: "Loading",
    levelOptions: {
      info: "Info",
      warning: "Warning",
      error: "Error",
    },
  },
};
