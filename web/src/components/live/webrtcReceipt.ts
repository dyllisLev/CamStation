export type InboundVideoReceipt = {
  readonly bytesReceived: number;
  readonly packetsReceived: number;
};

type StatsReportLike = {
  forEach(callback: (stat: unknown) => void): void;
};

export function inboundVideoReceipt(report: StatsReportLike): InboundVideoReceipt {
  let bytesReceived = 0;
  let packetsReceived = 0;
  report.forEach((value) => {
    if (!value || typeof value !== "object") return;
    const stat = value as Record<string, unknown>;
    if (stat.type !== "inbound-rtp" || (stat.kind !== "video" && stat.mediaType !== "video")) return;
    bytesReceived += nonNegativeNumber(stat.bytesReceived);
    packetsReceived += nonNegativeNumber(stat.packetsReceived);
  });
  return { bytesReceived, packetsReceived };
}

export function receiptAdvanced(
  previous: InboundVideoReceipt | null,
  current: InboundVideoReceipt,
): boolean {
  const baseline = previous ?? { bytesReceived: 0, packetsReceived: 0 };
  return current.bytesReceived > baseline.bytesReceived || current.packetsReceived > baseline.packetsReceived;
}

function nonNegativeNumber(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : 0;
}
