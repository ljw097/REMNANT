import { describe, expect, it } from "vitest";
import { SSEParser } from "./api/sse";
import { contrastOnWhite, defaultPrefs, normalizePrefs } from "./prefs";
import { match } from "./router";
import { parseExcerpt } from "./ui/domain";

describe("SSEParser", () => {
  it("parses events split across chunks and line endings", () => {
    const p = new SSEParser();
    expect(p.push("id: 7\r\nevent: run.qu")).toEqual([]);
    expect(p.push("eued\r\ndata: {\"a\":1}\r\n\r\n: keepalive\n\nid: 8\ndata: x\ndata: y\n\n")).toEqual([
      { id: "7", event: "run.queued", data: '{"a":1}' },
      { id: "8", event: "message", data: "x\ny" },
    ]);
  });

  it("keeps the last id for events without one", () => {
    const p = new SSEParser();
    expect(p.push("id: 3\nevent: end\n\nevent: end\n\n")).toEqual([
      { id: "3", event: "end", data: "" },
      { id: "3", event: "end", data: "" },
    ]);
  });
});

describe("match", () => {
  it("captures and decodes params", () => {
    expect(match("/runs/:id", "/runs/12")).toEqual({ id: "12" });
    expect(match("/settings/:section", "/settings/a%20b/")).toEqual({ section: "a b" });
  });
  it("rejects other shapes", () => {
    expect(match("/runs/:id", "/runs")).toBeNull();
    expect(match("/runs/:id", "/projects/1")).toBeNull();
    expect(match("/", "/")).toEqual({});
  });
});

describe("normalizePrefs", () => {
  it("falls back to role defaults", () => {
    expect(normalizePrefs(null, "reviewer", "ko")).toEqual(defaultPrefs("reviewer", "ko"));
    expect(normalizePrefs(null, "reviewer", "ko").startPage).toBe("/review");
  });

  it("drops invalid values and keeps every panel once", () => {
    const p = normalizePrefs(
      {
        theme: "neon",
        density: "compact",
        startPage: "https://evil.example",
        dashboard: [{ id: "projects", visible: false }, { id: "projects" }, { id: "bogus" }],
        columns: { runs: ["status", "nope", 3] },
      },
      "researcher",
      "en",
    );
    expect(p.theme).toBe("system");
    expect(p.density).toBe("compact");
    expect(p.startPage).toBe("/");
    expect(p.dashboard[0]).toEqual({ id: "projects", visible: false });
    expect(new Set(p.dashboard.map((d) => d.id)).size).toBe(p.dashboard.length);
    expect(p.dashboard.length).toBe(defaultPrefs("researcher", "en").dashboard.length);
    expect(p.columns.runs).toEqual(["status"].filter((c) => defaultPrefs("researcher", "en").columns.runs.includes(c)));
  });
});

describe("parseExcerpt", () => {
  it("splits numbered lines and the trailing note", () => {
    const { lines, note } = parseExcerpt("   15 | int f(void)\n   16 | {\n      | (truncated)\n");
    expect(lines).toEqual([
      { no: 15, src: "int f(void)" },
      { no: 16, src: "{" },
    ]);
    expect(note).toBe("(truncated)");
  });
  it("keeps unnumbered text", () => {
    expect(parseExcerpt("plain").lines).toEqual([{ no: null, src: "plain" }]);
  });
});

describe("contrastOnWhite", () => {
  it("matches the WCAG endpoints", () => {
    expect(contrastOnWhite("#ffffff")).toBeCloseTo(1, 5);
    expect(contrastOnWhite("#000000")).toBeCloseTo(21, 5);
    expect(contrastOnWhite("#767676")).toBeGreaterThan(4.5);
  });
});
