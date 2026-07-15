import { config } from "@/lib/config";
import { getToken } from "@/lib/token";

interface StreamHandlers {
  onOpen?: () => void;
  onEvent: (event: unknown) => void;
}

/**
 * Consume the SSE feed with fetch (not EventSource, which can't set the
 * Authorization header). Resolves when the stream ends or `signal` aborts;
 * rejects on a transport/HTTP error so the caller can reconnect.
 */
export async function openEventStream(
  { onOpen, onEvent }: StreamHandlers,
  signal: AbortSignal,
): Promise<void> {
  const res = await fetch(`${config.apiBaseUrl}/api/v1/stream`, {
    headers: { Authorization: `Bearer ${getToken() ?? ""}` },
    signal,
  });
  if (!res.ok || !res.body) throw new Error(`stream ${res.status}`);
  onOpen?.();

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) return;
    buf += decoder.decode(value, { stream: true });
    // SSE frames are separated by a blank line.
    let sep: number;
    while ((sep = buf.indexOf("\n\n")) !== -1) {
      const frame = buf.slice(0, sep);
      buf = buf.slice(sep + 2);
      for (const line of frame.split("\n")) {
        if (line.startsWith("data:")) {
          try {
            onEvent(JSON.parse(line.slice(5).trim()));
          } catch {
            /* ignore malformed frame */
          }
        }
      }
    }
  }
}
