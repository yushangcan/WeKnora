#!/usr/bin/env python3
"""Synthetic CI provider. No model quality, billing or latency claims."""
import json
import re
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

API_KEY = "ci-stub-key"
DIMENSION = 8
STATE_LOCK = threading.Lock()
STATE = {"mode": "normal", "chat": 0, "embedding": 0, "rerank": 0}
ANSWERS = {"france": "Paris", "japan": "Tokyo"}


def topic(text):
    words = re.findall(r"\b(france|japan)\b", text.lower())
    return words[-1] if words else ""


def embedding(text):
    vector = [0.0] * DIMENSION
    vector[{"france": 0, "japan": 1}.get(topic(text), 2)] = 1.0
    return vector


def text_content(message):
    content = message.get("content", "")
    if isinstance(content, str):
        return content
    return " ".join(p.get("text", "") for p in content if p.get("type") == "text")


def answer(messages, mode):
    # Only resolve a known fixture question from the last user message.
    # Unknown/enrichment prompts receive a stable neutral response.
    user_text = next((text_content(m) for m in reversed(messages) if m.get("role") == "user"), "")
    questions = re.findall(r"What is the capital of (France|Japan)\?", user_text, re.I)
    if not questions:
        return "CI synthetic response"
    return "Incorrect" if mode == "degraded" else ANSWERS[questions[-1].lower()]


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass  # Do not log headers, credentials or prompts.

    def send_json(self, status, payload):
        data = json.dumps(payload, ensure_ascii=False).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path == "/health":
            self.send_json(200, {"status": "ok", "provider": "synthetic-ci-v1"})
        elif self.path == "/stats":
            with STATE_LOCK:
                state = dict(STATE)
            self.send_json(200, state)
        else:
            self.send_json(404, {"error": "not_found"})

    def do_POST(self):
        if self.headers.get("Authorization") != "Bearer " + API_KEY:
            self.send_json(401, {"error": "invalid_api_key"})
            return
        try:
            size = int(self.headers.get("Content-Length", "0"))
            if not 0 < size <= 2 * 1024 * 1024:
                raise ValueError()
            body = json.loads(self.rfile.read(size))
            if not isinstance(body, dict):
                raise ValueError()
        except (ValueError, TypeError):
            self.send_json(400, {"error": "invalid_request"})
            return
        if self.path == "/control":
            if body.get("mode") not in ("normal", "degraded"):
                self.send_json(400, {"error": "invalid_mode"})
                return
            with STATE_LOCK:
                STATE["mode"] = body["mode"]
            self.send_json(200, {"success": True})
            return
        routes = {"/v1/chat/completions": ("ci-chat", "chat"),
                  "/v1/embeddings": ("ci-embedding", "embedding"),
                  "/v1/rerank": ("ci-rerank", "rerank")}
        if self.path not in routes:
            self.send_json(404, {"error": "not_found"})
            return
        model, operation = routes[self.path]
        if body.get("model") != model:
            self.send_json(400, {"error": "unexpected_model"})
            return
        with STATE_LOCK:
            STATE[operation] += 1
            mode = STATE["mode"]
        if operation == "embedding":
            inputs = body.get("input", [])
            inputs = [inputs] if isinstance(inputs, str) else inputs
            self.send_json(200, {
                "object": "list", "model": model,
                "data": [{"object": "embedding", "index": i, "embedding": embedding(t)}
                         for i, t in enumerate(inputs)],
                # Synthetic token counts; intentionally no monetary cost.
                "usage": {"prompt_tokens": len(inputs) * 8, "total_tokens": len(inputs) * 8}})
        elif operation == "rerank":
            query_topic = topic(body.get("query", ""))
            results = [{"index": i, "document": {"text": text},
                        "relevance_score": 0.99 if query_topic and query_topic == topic(text) else 0.01}
                       for i, text in enumerate(body.get("documents", []))]
            results.sort(key=lambda r: (-r["relevance_score"], r["document"]["text"]))
            self.send_json(200, {"id": "synthetic-rerank", "model": model,
                                 "results": results, "usage": {"total_tokens": 16}})
        else:
            response = answer(body.get("messages", []), mode)
            usage = {"prompt_tokens": 16, "completion_tokens": 4, "total_tokens": 20}
            if not body.get("stream"):
                self.send_json(200, {"id": "synthetic-chat", "object": "chat.completion",
                    "created": 1, "model": model, "usage": usage,
                    "choices": [{"index": 0, "message": {"role": "assistant", "content": response},
                                 "finish_reason": "stop"}]})
                return
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            chunks = [
                {"choices": [{"index": 0, "delta": {"role": "assistant", "content": response},
                              "finish_reason": None}]},
                {"choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}]},
                {"choices": [], "usage": usage},
            ]
            for chunk in chunks:
                chunk.update(id="synthetic-chat", object="chat.completion.chunk", created=1, model=model)
                self.wfile.write(("data: " + json.dumps(chunk) + "\n\n").encode())
            self.wfile.write(b"data: [DONE]\n\n")
            self.wfile.flush()


if __name__ == "__main__":
    print("synthetic-ci-v1 listening on :8090", flush=True)
    ThreadingHTTPServer(("0.0.0.0", 8090), Handler).serve_forever()
