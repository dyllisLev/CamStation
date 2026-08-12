import assert from "node:assert/strict";
import test from "node:test";
import { inboundVideoReceipt, receiptAdvanced } from "../src/components/live/webrtcReceipt.ts";

test("extracts only local inbound video RTP receipt counters", () => {
  const stats = new Map<string, unknown>([
    ["video-1", { type: "inbound-rtp", kind: "video", bytesReceived: 100, packetsReceived: 5 }],
    ["video-2", { type: "inbound-rtp", mediaType: "video", bytesReceived: 40, packetsReceived: 2 }],
    ["audio", { type: "inbound-rtp", kind: "audio", bytesReceived: 999, packetsReceived: 99 }],
    ["remote", { type: "remote-inbound-rtp", kind: "video", bytesReceived: 999, packetsReceived: 99 }],
  ]);

  assert.deepEqual(inboundVideoReceipt(stats), { bytesReceived: 140, packetsReceived: 7 });
});

test("network receipt advances independently when bytes or packets increase", () => {
  const previous = { bytesReceived: 100, packetsReceived: 5 };

  assert.equal(receiptAdvanced(previous, { bytesReceived: 101, packetsReceived: 5 }), true);
  assert.equal(receiptAdvanced(previous, { bytesReceived: 100, packetsReceived: 6 }), true);
  assert.equal(receiptAdvanced(previous, { bytesReceived: 100, packetsReceived: 5 }), false);
  assert.equal(receiptAdvanced(previous, { bytesReceived: 1, packetsReceived: 1 }), false);
  assert.equal(receiptAdvanced(null, { bytesReceived: 1, packetsReceived: 1 }), true);
});
