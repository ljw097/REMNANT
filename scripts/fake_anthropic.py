#!/usr/bin/env python3
"""Minimal stand-in for POST /v1/messages, used by scripts/smoke.sh.

It answers as the Hypothesis Researcher or the Skeptic by reading the task
envelope remnantd sends, so the smoke test exercises the real SDK client,
structured-output request and response handling without network access.
GET /calls returns how many requests were served per role; POST /delay?s=N
makes every later answer take N seconds (used to interrupt a run mid-stage).
"""
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

calls = {"researcher": 0, "skeptic": 0}
delay = [0.0]


def answer(system, envelope):
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
