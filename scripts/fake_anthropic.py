#!/usr/bin/env python3
"""Minimal stand-in for POST /v1/messages, used by scripts/smoke.sh.

It answers as the Historian, the Hypothesis Researcher or the Skeptic by
reading the task envelope remnantd sends, so the smoke test exercises the real SDK client,
structured-output request and response handling without network access.
GET /calls returns how many requests were served per role; POST /delay?s=N
makes every later answer take N seconds (used to interrupt a run mid-stage).
"""
import json
import re
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

calls = {"researcher": 0, "skeptic": 0}
delay = [0.0]


KEYWORDS = {"if", "for", "while", "switch", "return", "sizeof", "else", "case", "do", "goto", "defined"}


def historian(envelope):
    """A synthetic pattern from a fix commit: the first called identifiers
    on the pre-fix side of the diff become the code signatures."""
    calls["historian"] = calls.get("historian", 0) + 1
    commit = envelope["commit"]
    before, added = [], []
    for line in commit["diff"].splitlines():
        if line.startswith(("+++", "---", "@@", "diff ", "index ")):
            continue
        (added if line.startswith("+") else before).append(line[1:])
    called = lambda lines: [m for l in lines for m in re.findall(r"\b([A-Za-z_]\w{2,})\s*\(", l) if m not in KEYWORDS]
    sigs = list(dict.fromkeys(called(before)))[:3]
    counters = [c for c in dict.fromkeys(called(added)) if c not in sigs][:2]
    if not sigs:
        return {"security_fix": False, "reason": "no call to anchor a pattern on", "title": "", "vulnerability_class": "",
                "cwe_id": "", "root_cause": "", "preconditions": [], "code_signatures": [], "counter_signatures": [], "fix_summary": ""}
    return {
        "security_fix": True,
        "reason": "synthetic answer from scripts/fake_anthropic.py",
        "title": "Synthetic: " + commit["subject"][:120],
        "vulnerability_class": "memory-safety",
        "cwe_id": "",
        "root_cause": "SYNTHETIC (fake model): the fix in this commit adds a check around " + ", ".join(sigs) + ".",
        "preconditions": ["synthetic demo answer"],
        "code_signatures": sigs,
        "counter_signatures": counters,
        "fix_summary": commit["subject"][:200],
    }


def answer(system, envelope):
    if "You are the Historian" in system:
        return historian(envelope)
    ev = envelope["evidence"][0]
    if "Hypothesis Researcher" in system:
        calls["researcher"] += 1
        return {
            "found": True,
            "title": "Length not checked before copy",
            "claim": "parse copies len bytes into a fixed buffer without comparing len to its size.",
            "vulnerability_class": "memory-safety",
            "cwe_id": "CWE-787",
            "facts": ["memcpy uses the caller-supplied len"],
            "preconditions": ["len exceeds sizeof(r->name)"],
            "unknowns": ["whether callers bound len"],
            "affected_symbols": [ev["symbol"] or "parse"],
            "expected_observation": "ASan reports a stack or heap overflow in parse",
            "supporting_evidence_ids": [ev["evidence_id"]],
            "counter_evidence_to_check": ["callers clamp len"],
            "triage_confidence": "high",
        }
    calls["skeptic"] += 1
    return {
        "verdict": "supports",
        "rationale": "No bound on len is visible in the excerpt.",
        "counter_evidence": [],
        "adjusted_confidence": "high",
    }


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def reply(self, code, body):
        data = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        try:
            self.wfile.write(data)
        except (BrokenPipeError, ConnectionResetError):
            pass  # the client went away, e.g. remnantd killed by the crash test

    def do_GET(self):
        # GET /v1/models/{id}: the connection check of a model profile.
        if self.path.startswith("/v1/models/"):
            if not self.headers.get("Authorization", "").startswith("Bearer sk-ant-"):
                return self.reply(401, {"type": "error", "error": {"type": "authentication_error", "message": "bad key"}})
            model = self.path.rsplit("/", 1)[1]
            if not model.startswith("claude-"):
                return self.reply(404, {"type": "error", "error": {"type": "not_found_error", "message": "model not found"}})
            return self.reply(200, {"type": "model", "id": model, "display_name": model + " (fake)", "created_at": "2026-01-01T00:00:00Z"})
        self.reply(200, calls)

    def do_POST(self):
        url = urlparse(self.path)
        if url.path == "/delay":
            delay[0] = float(parse_qs(url.query)["s"][0])
            return self.reply(200, {"delay": delay[0]})
        time.sleep(delay[0])
        req = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if self.path != "/v1/messages" or not self.headers.get("Authorization", "").startswith("Bearer sk-ant-"):
            return self.reply(401, {"type": "error", "error": {"type": "authentication_error", "message": "bad key"}})
        if req.get("output_config", {}).get("format", {}).get("type") != "json_schema":
            return self.reply(400, {"type": "error", "error": {"type": "invalid_request_error", "message": "no schema"}})
        system = "".join(b["text"] for b in req["system"])
        text = req["messages"][0]["content"][0]["text"]
        envelope = json.loads(text.split("\n\n", 1)[1])
        self.reply(200, {
            "id": "msg_fake", "type": "message", "role": "assistant", "model": req["model"],
            "content": [{"type": "text", "text": json.dumps(answer(system, envelope))}],
            "stop_reason": "end_turn", "stop_sequence": None,
            "usage": {"input_tokens": 1000, "output_tokens": 200},
        })


ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
