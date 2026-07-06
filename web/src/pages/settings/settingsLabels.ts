import type { Language } from "../../app/language";

type SettingsLabels = {
  readonly title: string;
  readonly subtitle: string;
  readonly language: string;
  readonly recording: string;
  readonly backup: string;
  readonly alerts: string;
  readonly segmentMinutes: string;
  readonly retentionDays: string;
  readonly maxStorageGB: string;
  readonly enabled: string;
  readonly target: string;
  readonly scheduleEnabled: string;
  readonly scheduleCron: string;
  readonly protectUnbacked: string;
  readonly discordEnabled: string;
  readonly webhook: string;
  readonly webhookState: string;
  readonly hasSecret: string;
  readonly noSecret: string;
  readonly fingerprint: string;
  readonly save: string;
  readonly reset: string;
  readonly confirmReset: string;
  readonly testAlert: string;
  readonly dryRun: string;
  readonly successSave: string;
  readonly successReset: string;
  readonly successTest: string;
  readonly loading: string;
  readonly errorPrefix: string;
};

export const settingsLabels: Record<Language, SettingsLabels> = {
  ko: {
    title: "운영 설정",
    subtitle: "녹화, 백업, 알림 값을 저장하고 비밀값은 마스킹 상태만 표시합니다.",
    language: "언어",
    recording: "녹화",
    backup: "백업",
    alerts: "알림",
    segmentMinutes: "세그먼트 분",
    retentionDays: "보관일",
    maxStorageGB: "최대 GB",
    enabled: "사용",
    target: "대상",
    scheduleEnabled: "스케줄 실행",
    scheduleCron: "스케줄 cron (KST)",
    protectUnbacked: "미백업 영상 보호",
    discordEnabled: "Discord 알림",
    webhook: "새 웹훅",
    webhookState: "웹훅 상태",
    hasSecret: "저장됨",
    noSecret: "없음",
    fingerprint: "지문",
    save: "저장",
    reset: "초기화",
    confirmReset: "다시 눌러 초기화",
    testAlert: "알림 테스트",
    dryRun: "저장된 웹훅으로 실제 테스트 알림을 보냅니다.",
    successSave: "설정을 저장했습니다.",
    successReset: "설정을 초기화했습니다.",
    successTest: "알림 테스트를 보냈습니다.",
    loading: "불러오는 중",
    errorPrefix: "요청 실패",
  },
  en: {
    title: "Operation Settings",
    subtitle: "Save recording, backup, and alert values while showing only masked secret state.",
    language: "Language",
    recording: "Recording",
    backup: "Backup",
    alerts: "Alerts",
    segmentMinutes: "Segment minutes",
    retentionDays: "Retention days",
    maxStorageGB: "Max GB",
    enabled: "Enabled",
    target: "Target",
    scheduleEnabled: "Scheduled backup",
    scheduleCron: "Schedule cron (KST)",
    protectUnbacked: "Protect unbacked recordings",
    discordEnabled: "Discord alerts",
    webhook: "New webhook",
    webhookState: "Webhook state",
    hasSecret: "Saved",
    noSecret: "None",
    fingerprint: "Fingerprint",
    save: "Save",
    reset: "Reset",
    confirmReset: "Confirm reset",
    testAlert: "Test alert",
    dryRun: "Sends a real test alert to the saved webhook.",
    successSave: "Settings saved.",
    successReset: "Settings reset.",
    successTest: "Test alert sent.",
    loading: "Loading",
    errorPrefix: "Request failed",
  },
};
