import assert from "node:assert/strict";
import test from "node:test";
import { preferredLiveTransport } from "../src/components/live/preferredLiveTransport.ts";

test("direct private, loopback and link-local addresses retain WebRTC first", () => {
  for (const host of ["localhost", "LOCALHOST.", "127.0.0.1", "127.20.30.40", "10.0.0.26", "192.168.0.160", "172.16.0.1", "172.31.255.254", "169.254.1.2", "[::1]", "[0:0:0:0:0:0:0:1]", "fc00::1", "[FD12::1234]", "[fe80::1]", "[febf::1]"]) {
    assert.equal(preferredLiveTransport(host), "webrtc", host);
  }
});

test("hostname and public address access explicitly prefer the HTTP media proxy", () => {
  for (const host of ["cctv2.nuc.hmini.me", "camstation.lan", "camera.local", "localhost.example.com", "192.168.0.160.example.com", "8.8.8.8", "172.15.0.1", "172.32.0.1", "169.255.0.1", "192.169.0.1", "[2001:db8::1]", "[fe7f::1]", "[fec0::1]", "[::]", "", "10.999.0.1", "[fd00::invalid]"]) {
    assert.equal(preferredLiveTransport(host), "mse", host);
  }
});
