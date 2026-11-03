// Server-Sent Events over fetch with the session cookie. Reconnects with
// Last-Event-ID until the server sends "end" or the caller aborts.

import { authHeaders } from "./client";
import type { RunEvent } from "./types";

export interface SSEMessage {
  id?: string;
  event: string;
  data: string;
}

/** Incremental parser for the text/event-stream format. */
export class SSEParser {
  private buf = "";
  private id: string | undefined;
  private event = "";
  private data: string[] = [];

  push(chunk: string): SSEMessage[] {
    this.buf += chunk;
    const out: SSEMessage[] = [];
    let nl: number;
    while ((nl = this.buf.search(/\r\n|\r|\n/)) >= 0) {
      const line = this.buf.slice(0, nl);
      const sepLen = this.buf.startsWith("\r\n", nl) ? 2 : 1;
      this.buf = this.buf.slice(nl + sepLen);
      if (line === "") {
        if (this.data.length > 0 || this.event) {
          out.push({ id: this.id, event: this.event || "message", data: this.data.join("\n") });
        }
        this.event = "";
        this.data = [];
        continue;
      }
      if (line.startsWith(":")) continue;
      const colon = line.indexOf(":");
      const field = colon < 0 ? line : line.slice(0, colon);
      let value = colon < 0 ? "" : line.slice(colon + 1);
      if (value.startsWith(" ")) value = value.slice(1);
      if (field === "data") this.data.push(value);
      else if (field === "event") this.event = value;
      else if (field === "id") this.id = value;
    }
    return out;
  }
}

export interface StreamHandlers {
  onEvent: (e: RunEvent) => void;
  onEnd?: (status: string) => void;
  onState?: (state: "live" | "reconnecting") => void;
}

export async function streamRunEvents(runId: number, signal: AbortSignal, h: StreamHandlers): Promise<void> {
  let lastId = "";
  let backoff = 1000;
  while (!signal.aborted) {
    try {
      const headers: Record<string, string> = { Accept: "text/event-stream", ...authHeaders() };
      if (lastId) headers["Last-Event-ID"] = lastId;
      const res = await fetch(`/api/v1/runs/${runId}/events`, { headers, signal, credentials: "same-origin" });
      if (!res.ok || !res.body) throw new Error(`events: ${res.status}`);
      h.onState?.("live");
      backoff = 1000;
      const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
      const parser = new SSEParser();
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        for (const m of parser.push(value)) {
          if (m.id) lastId = m.id;
          if (m.event === "end") {
            let status = "";
            try {
              status = (JSON.parse(m.data) as { status: string }).status;
            } catch {
              // keep empty
            }
            h.onEnd?.(status);
            return;
          }
          try {
            h.onEvent(JSON.parse(m.data) as RunEvent);
          } catch {
            // ignore malformed lines
          }
        }
      }
    } catch (e) {
      if (signal.aborted) return;
      void e;
    }
    h.onState?.("reconnecting");
    await new Promise((r) => setTimeout(r, backoff));
    backoff = Math.min(backoff * 2, 15_000);
  }
}
