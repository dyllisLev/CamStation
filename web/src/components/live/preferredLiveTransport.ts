import type { PlaybackTransport } from "./playbackRecovery";

// This is a default routing policy, not an ICE reachability test. Hostname
// access (including LAN DNS names) starts through the existing HTTP proxy.
// Direct private/loopback/link-local IP access retains WebRTC first.
export function preferredLiveTransport(hostname: string): PlaybackTransport {
  const host = hostname.toLowerCase();
  if (host === "localhost" || host === "localhost.") return "webrtc";
  if (/^\d+\.\d+\.\d+\.\d+$/u.test(host)) {
    const bytes = host.split(".").map(Number);
    if (bytes.some((byte) => byte > 255)) return "mse";
    const [a, b] = bytes;
    return a === 10 || a === 127 || (a === 172 && b >= 16 && b <= 31)
      || (a === 192 && b === 168) || (a === 169 && b === 254) ? "webrtc" : "mse";
  }
  const literal = host.startsWith("[") && host.endsWith("]") ? host.slice(1, -1) : host;
  if (literal.includes(":") && /^[\da-f:.]+$/u.test(literal)) {
    try {
      const ipv6 = new URL(`http://[${literal}]/`).hostname;
      if (ipv6 === "[::1]" || /^\[f[cd][\da-f]{2}:/u.test(ipv6) || /^\[fe[89ab][\da-f]:/u.test(ipv6)) return "webrtc";
    } catch {
      // Invalid literals use the normal proxy preference.
    }
  }
  return "mse";
}
