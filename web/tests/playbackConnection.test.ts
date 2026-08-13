import assert from "node:assert/strict";
import test from "node:test";
import { PlaybackProgressVerifier } from "../src/components/live/playbackConnection.ts";

test("primary recovery requires continuous video-clock progress instead of one timestamp jump", () => {
  const verifier = new PlaybackProgressVerifier();

  assert.equal(verifier.observe(Number.NaN), false);
  assert.equal(verifier.observe(120), false, "the first decoded timestamp only establishes a baseline");
  assert.equal(verifier.observe(120.75), false);
  assert.equal(verifier.observe(121), true);
});

test("a backwards video-clock reset starts a new primary recovery baseline", () => {
  const verifier = new PlaybackProgressVerifier();

  assert.equal(verifier.observe(50), false);
  assert.equal(verifier.observe(50.5), false);
  assert.equal(verifier.observe(2), false);
  assert.equal(verifier.observe(2.9), false);
  assert.equal(verifier.observe(3), true);
});
