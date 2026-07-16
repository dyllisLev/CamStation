export type MseControlMessage =
  | { readonly type: "mse"; readonly value: string }
  | { readonly type: "error" | "invalid"; readonly value: "" };

export function parseMseControlMessage(raw: string): MseControlMessage {
  try {
    const message: unknown = JSON.parse(raw);
    if (!message || typeof message !== "object" || !("type" in message)) return { type: "invalid", value: "" };
    if (message.type === "mse" && "value" in message && typeof message.value === "string") {
      return { type: "mse", value: message.value };
    }
    if (message.type === "error") return { type: "error", value: "" };
    return { type: "invalid", value: "" };
  } catch {
    return { type: "invalid", value: "" };
  }
}
