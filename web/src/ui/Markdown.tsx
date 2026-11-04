// A small, CSP-safe Markdown renderer. It builds React elements only — no
// innerHTML, no dangerouslySetInnerHTML — so it works under the app's strict
// CSP. It covers the subset the REMNANT reports use: headings, paragraphs,
// blockquotes, GFM pipe tables, ordered/unordered lists, fenced and inline
// code, bold/italic, and links.

import type { ReactNode } from "react";
import { navigate } from "../router";

// Entity references the REMNANT report writes — [H-2], [R-4], [P-3] — and CVE
// ids. When `refs` is on they become links to the detail page (or NVD), so a
// report reads like the app: click a reference to open what it names.
const refTo: Record<string, (n: string) => string> = {
  H: (n) => `/hypotheses/${n}`,
  R: (n) => `/runs/${n}`,
  P: (n) => `/patterns/${n}`,
};

function RefLink({ to, children }: { to: string; children: ReactNode }) {
  const external = /^https?:/.test(to);
  return (
    <a
      href={to}
      className="md-ref"
      {...(external ? { target: "_blank", rel: "noopener noreferrer" } : {})}
      onClick={(e) => {
        if (external || e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
        e.preventDefault();
        navigate(to);
      }}
    >
      {children}
    </a>
  );
}

type Block =
  | { t: "heading"; level: number; text: string }
  | { t: "p"; text: string }
  | { t: "quote"; lines: string[] }
  | { t: "code"; lang: string; lines: string[] }
  | { t: "table"; header: string[]; rows: string[][] }
  | { t: "ul"; items: string[] }
  | { t: "ol"; items: string[] }
  | { t: "hr" };

const isTableSep = (s: string) => /^\s*\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)*\|?\s*$/.test(s) && s.includes("-");
const splitRow = (s: string) =>
  s
    .trim()
    .replace(/^\|/, "")
    .replace(/\|$/, "")
    .split("|")
    .map((c) => c.trim());

function parse(src: string): Block[] {
  const lines = src.replace(/\r\n?/g, "\n").split("\n");
  const blocks: Block[] = [];
  let i = 0;
  const at = (k: number) => lines[k] ?? "";
  while (i < lines.length) {
    let line = at(i);
    if (line.trim() === "") {
      i++;
      continue;
    }
    // Fenced code.
    const fence = line.match(/^```(.*)$/);
    if (fence) {
      const lang = (fence[1] ?? "").trim();
      const body: string[] = [];
      i++;
      while (i < lines.length && !/^```/.test(at(i))) body.push(at(i++));
      i++; // closing fence
      blocks.push({ t: "code", lang, lines: body });
      continue;
    }
    // Horizontal rule.
    if (/^(\s*[-*_]){3,}\s*$/.test(line) && !line.trim().startsWith("|")) {
      blocks.push({ t: "hr" });
      i++;
      continue;
    }
    // Heading.
    const h = line.match(/^(#{1,6})\s+(.*)$/);
    if (h) {
      blocks.push({ t: "heading", level: (h[1] ?? "").length, text: (h[2] ?? "").trim() });
      i++;
      continue;
    }
    // Blockquote.
    if (/^\s*>/.test(line)) {
      const q: string[] = [];
      while (i < lines.length && /^\s*>/.test(at(i))) q.push(at(i++).replace(/^\s*>\s?/, ""));
      blocks.push({ t: "quote", lines: q });
      continue;
    }
    // GFM table: a header row followed by a separator row.
    if (line.includes("|") && i + 1 < lines.length && isTableSep(at(i + 1))) {
      const header = splitRow(line);
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length && at(i).includes("|") && at(i).trim() !== "") rows.push(splitRow(at(i++)));
      blocks.push({ t: "table", header, rows });
      continue;
    }
    // Lists.
    if (/^\s*[-*+]\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^\s*[-*+]\s+/.test(at(i))) items.push(at(i++).replace(/^\s*[-*+]\s+/, ""));
      blocks.push({ t: "ul", items });
      continue;
    }
    if (/^\s*\d+\.\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^\s*\d+\.\s+/.test(at(i))) items.push(at(i++).replace(/^\s*\d+\.\s+/, ""));
      blocks.push({ t: "ol", items });
      continue;
    }
    // Paragraph: gather until a blank line or a block starter.
    const para: string[] = [];
    while (i < lines.length && at(i).trim() !== "" && !/^(#{1,6}\s|```|\s*>|\s*[-*+]\s|\s*\d+\.\s)/.test(at(i))) {
      para.push(at(i++));
    }
    line = para.join(" ");
    if (line.trim()) blocks.push({ t: "p", text: line });
  }
  return blocks;
}

// Inline formatting: `code`, **bold**, *italic* / _italic_, [text](url).
// Code spans win first so markup inside them stays literal.
function inline(text: string, keyBase: string, refs = false): ReactNode[] {
  const out: ReactNode[] = [];
  const re = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^*]+\*)|(_[^_]+_)|(\[[^\]]+\]\([^)]+\))|(\[[HRP]-\d+\])|(\bCVE-\d{4}-\d{4,7}\b)/g;
  let last = 0;
  let m: RegExpExecArray | null;
  let n = 0;
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) out.push(text.slice(last, m.index));
    const tok = m[0];
    const k = `${keyBase}-${n++}`;
    const ref = tok.match(/^\[([HRP])-(\d+)\]$/);
    const cve = /^CVE-/.test(tok);
    if (tok.startsWith("`")) out.push(<code key={k} className="md-code">{tok.slice(1, -1)}</code>);
    else if (tok.startsWith("**")) out.push(<strong key={k}>{tok.slice(2, -2)}</strong>);
    else if (tok.startsWith("*")) out.push(<em key={k}>{tok.slice(1, -1)}</em>);
    else if (tok.startsWith("_")) out.push(<em key={k}>{tok.slice(1, -1)}</em>);
    else if (ref) out.push(refs ? <RefLink key={k} to={refTo[ref[1]!]!(ref[2]!)}>{tok}</RefLink> : <span key={k} className="md-ref-flat">{tok}</span>);
    else if (cve) out.push(refs ? <RefLink key={k} to={`https://nvd.nist.gov/vuln/detail/${tok}`}>{tok}</RefLink> : <span key={k}>{tok}</span>);
    else {
      const lm = tok.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
      const label = lm?.[1] ?? tok;
      const href = lm?.[2] ?? "";
      const safe = /^(https?:|mailto:|\/)/i.test(href) ? href : "#";
      out.push(
        <a key={k} href={safe} target="_blank" rel="noopener noreferrer">
          {label}
        </a>,
      );
    }
    last = re.lastIndex;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

export function Markdown({ source, refs = false }: { source: string; refs?: boolean }) {
  const blocks = parse(source);
  const il = (text: string, key: string) => inline(text, key, refs);
  return (
    <div className="md">
      {blocks.map((b, i) => {
        const key = `b${i}`;
        switch (b.t) {
          case "heading": {
            const H = `h${Math.min(b.level + 1, 6)}` as "h2" | "h3" | "h4" | "h5" | "h6";
            return <H key={key}>{il(b.text, key)}</H>;
          }
          case "p":
            return <p key={key}>{il(b.text, key)}</p>;
          case "hr":
            return <hr key={key} />;
          case "quote":
            return (
              <blockquote key={key}>
                {b.lines.map((l, j) => (
                  <p key={j}>{il(l, `${key}-${j}`)}</p>
                ))}
              </blockquote>
            );
          case "code":
            return (
              <pre key={key} className="md-pre">
                <code>{b.lines.join("\n")}</code>
              </pre>
            );
          case "ul":
            return (
              <ul key={key}>
                {b.items.map((it, j) => (
                  <li key={j}>{il(it, `${key}-${j}`)}</li>
                ))}
              </ul>
            );
          case "ol":
            return (
              <ol key={key}>
                {b.items.map((it, j) => (
                  <li key={j}>{il(it, `${key}-${j}`)}</li>
                ))}
              </ol>
            );
          case "table":
            return (
              <div className="table-wrap" key={key}>
                <table className="table md-table">
                  <thead>
                    <tr>
                      {b.header.map((c, j) => (
                        <th key={j}>{il(c, `${key}-h${j}`)}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {b.rows.map((row, j) => (
                      <tr key={j}>
                        {row.map((c, k) => (
                          <td key={k}>{il(c, `${key}-${j}-${k}`)}</td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            );
        }
      })}
    </div>
  );
}
