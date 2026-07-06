import { createContext, type ReactNode, useEffect, useMemo, useState } from "react";
import type { Language } from "./language";

type Dictionary = Record<string, string>;

const dictionaries: Record<Language, Dictionary> = {
  ko: {
    controlRoom: "관제실",
    live: "라이브",
    recordings: "녹화",
    cameras: "카메라",
    incidents: "장애/알림",
    streams: "스트림",
    backup: "백업",
    viewers: "뷰어",
    logs: "로그",
    system: "시스템",
    settings: "설정",
    refresh: "새로고침",
    checking: "확인 중",
    queued: "대기",
    running: "실행 중",
    succeeded: "완료",
    failed: "실패",
    cancelled: "취소됨",
    deleted: "삭제됨",
    stopped: "중지됨",
    streaming: "송출 중",
    api: "API",
    console: "콘솔",
    language: "언어",
    languageSettings: "언어 설정",
    korean: "한국어",
    english: "영어",
    applyImmediately: "선택 즉시 화면 언어가 변경됩니다.",
    nvrSettings: "NVR 설정",
    segmentLength: "녹화 조각 길이",
    segmentLengthDetail: "녹화 파일을 몇 분 단위로 나눌지 정합니다.",
    retentionDays: "보관 기간",
    retentionDaysDetail: "녹화 파일 정리, 저장공간 경고, 타임라인 표시의 기준입니다.",
    maxStorage: "최대 저장공간",
    maxStorageDetail: "녹화 저장소가 사용할 수 있는 상한을 명확히 정해야 합니다.",
    alertRules: "알림 규칙",
    alertRulesDetail: "웹훅, 쿨다운, 확인, 일시중지 정책을 관리합니다.",
    secrets: "비밀값",
    secretsDetail: "RTSP 계정과 웹훅 토큰은 내보내기에서 안전하게 다뤄야 합니다.",
    importExport: "가져오기/내보내기",
    importExportDetail: "카메라 가져오기를 먼저 구현하고 녹화 메타데이터는 이후에 다룹니다.",
    profiles: "프로파일",
    profilesDetail: "카메라 제조사 기본값, 전송 방식, keepalive 정책을 관리합니다.",
    unknown: "미구현",
    offline: "오프라인",
    error: "오류",
    info: "정보",
    warning: "경고",
    degraded: "저하",
    open: "열림",
    acknowledged: "확인됨",
    snoozed: "일시중지",
    resolved: "해결됨",
    low: "낮음",
    medium: "보통",
    high: "높음",
    critical: "긴급",
  },
  en: {
    controlRoom: "Control Room",
    live: "Live",
    recordings: "Recordings",
    cameras: "Cameras",
    incidents: "Incidents",
    streams: "Streams",
    backup: "Backup",
    viewers: "Viewers",
    logs: "Logs",
    system: "System",
    settings: "Settings",
    refresh: "Refresh",
    checking: "checking",
    queued: "queued",
    running: "running",
    succeeded: "succeeded",
    failed: "failed",
    cancelled: "cancelled",
    deleted: "deleted",
    stopped: "stopped",
    streaming: "streaming",
    api: "API",
    console: "console",
    language: "Language",
    languageSettings: "Language Settings",
    korean: "Korean",
    english: "English",
    applyImmediately: "The interface language changes immediately.",
    nvrSettings: "NVR Settings",
    segmentLength: "Segment Length",
    segmentLengthDetail: "Recording segment policy will move from legacy settings into Go.",
    retentionDays: "Retention Days",
    retentionDaysDetail: "Retention policy should drive cleanup, storage warnings, and UI timeline.",
    maxStorage: "Max Storage",
    maxStorageDetail: "Storage cap handling must be explicit before production recording.",
    alertRules: "Alert Rules",
    alertRulesDetail: "Webhook, cooldown, acknowledge, and snooze settings belong here.",
    secrets: "Secrets",
    secretsDetail: "RTSP and webhook secrets need export-safe handling.",
    importExport: "Import/Export",
    importExportDetail: "Camera import comes first; recording metadata import comes later.",
    profiles: "Profiles",
    profilesDetail: "Camera profile defaults should cover transport and keepalive policies.",
    unknown: "unknown",
    offline: "offline",
    error: "error",
    info: "info",
    warning: "warning",
    degraded: "degraded",
    open: "open",
    acknowledged: "acknowledged",
    snoozed: "snoozed",
    resolved: "resolved",
    low: "low",
    medium: "medium",
    high: "high",
    critical: "critical",
  },
};

type LanguageContextValue = {
  language: Language;
  setLanguage: (language: Language) => void;
  t: (key: string) => string;
};

const LanguageContext = createContext<LanguageContextValue | null>(null);

export function LanguageProvider({ children }: { children: ReactNode }) {
  const [language, setLanguageState] = useState<Language>(() => {
    const saved = window.localStorage.getItem("camstation.language");
    return saved === "en" || saved === "ko" ? saved : "ko";
  });

  useEffect(() => {
    window.localStorage.setItem("camstation.language", language);
    document.documentElement.lang = language;
  }, [language]);

  const value = useMemo<LanguageContextValue>(
    () => ({
      language,
      setLanguage: setLanguageState,
      t: (key: string) => dictionaries[language][key] ?? key,
    }),
    [language],
  );

  return <LanguageContext.Provider value={value}>{children}</LanguageContext.Provider>;
}

export { LanguageContext };
