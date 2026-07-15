import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { openEventStream } from "@/lib/liveStream";

/**
 * Subscribes to the SSE feed and refreshes event/stats queries as new events
 * land, so the timeline and dashboard update without polling. Invalidations are
 * coalesced (bursts of events → one refetch) and the stream auto-reconnects.
 * Returns the live-connection state for a header indicator.
 */
export function useLiveUpdates(): boolean {
  const qc = useQueryClient();
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const ctrl = new AbortController();
    let stopped = false;
    let coalesce: ReturnType<typeof setTimeout> | undefined;

    const onEvent = () => {
      if (coalesce) return; // already scheduled
      coalesce = setTimeout(() => {
        coalesce = undefined;
        qc.invalidateQueries({ queryKey: ["events"] });
        qc.invalidateQueries({ queryKey: ["stats"] });
        qc.invalidateQueries({ queryKey: ["matrix"] });
      }, 1500);
    };

    async function run() {
      while (!stopped) {
        try {
          await openEventStream({ onOpen: () => setConnected(true), onEvent }, ctrl.signal);
        } catch {
          /* fall through to reconnect */
        }
        setConnected(false);
        if (stopped) break;
        await new Promise((r) => setTimeout(r, 3000)); // backoff before reconnect
      }
    }
    run();

    return () => {
      stopped = true;
      ctrl.abort();
      if (coalesce) clearTimeout(coalesce);
    };
  }, [qc]);

  return connected;
}
