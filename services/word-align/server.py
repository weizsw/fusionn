"""HTTP word alignment for English source subtitles.

Run this service beside the Go container. The model loads on the first request.
Only audio clips and their existing transcripts enter the model.
"""

from __future__ import annotations

import io
import json
import logging
import os
import re
import signal
import sys
import threading
import time
import uuid
from concurrent.futures import ProcessPoolExecutor
from concurrent.futures.process import BrokenProcessPool
from datetime import datetime, timezone
from email import policy
from email.parser import BytesParser
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from multiprocessing import get_context

MAX_REQUEST_BYTES = 4 * 1024 * 1024
MODEL_ID = os.environ.get("ALIGN_MODEL", "Qwen/Qwen3-ForcedAligner-0.6B-hf")
LOG = logging.getLogger("word-align")

_REQUEST_ID_RE = re.compile(r"[A-Za-z0-9._-]{1,64}\Z")
_NOISY_LOGGERS = ("transformers", "huggingface_hub", "httpx", "httpcore", "filelock", "urllib3", "torch")


class _UTCFormatter(logging.Formatter):
    def formatTime(self, record, datefmt=None):
        moment = datetime.fromtimestamp(record.created, tz=timezone.utc)
        return moment.strftime("%Y-%m-%dT%H:%M:%S.") + f"{moment.microsecond // 1000:03d}Z"


def configure_logging():
    """Set up the service logger on stderr. Idempotent and safe in each process."""
    if LOG.handlers:
        return
    level = os.environ.get("ALIGN_LOG_LEVEL", "INFO").upper()
    LOG.setLevel(logging.getLevelNamesMapping().get(level, logging.INFO))
    handler = logging.StreamHandler()  # stderr
    handler.setFormatter(_UTCFormatter("%(asctime)s %(levelname)s pid=%(process)d %(message)s"))
    LOG.addHandler(handler)
    LOG.propagate = False
    for name in _NOISY_LOGGERS:
        logging.getLogger(name).setLevel(logging.WARNING)
    os.environ.setdefault("HF_HUB_DISABLE_PROGRESS_BARS", "1")


class ModelAligner:
    def __init__(self):
        self.model = None
        self.processor = None
        self.lock = threading.Lock()

    def align(self, audio_bytes: bytes, text: str, request_id: str | None = None) -> list[dict]:
        import soundfile as sf
        import torch
        from transformers import AutoModelForTokenClassification, AutoProcessor
        from transformers.utils.logging import disable_progress_bar

        disable_progress_bar()

        audio, sample_rate = sf.read(io.BytesIO(audio_bytes), dtype="float32")
        if sample_rate != 16000 or audio.ndim != 1 or not 0 < len(audio) <= 31 * 16000:
            raise ValueError("audio must be mono 16 kHz WAV and at most 31 seconds")
        if not text.strip() or len(text) > 4096:
            raise ValueError("text must contain 1 to 4096 characters")
        with self.lock:
            if self.model is None:
                torch.set_num_threads(int(os.environ.get("ALIGN_THREADS", "4")))
                LOG.info("event=model_load_started model=%s request_id=%s", MODEL_ID, request_id)
                load_start = time.perf_counter()
                processor = AutoProcessor.from_pretrained(MODEL_ID)
                model = AutoModelForTokenClassification.from_pretrained(
                    MODEL_ID, dtype=torch.float32, attn_implementation="sdpa"
                ).eval()
                self.processor, self.model = processor, model
                LOG.info(
                    "event=model_load_completed model=%s device=%s load_ms=%s request_id=%s",
                    MODEL_ID,
                    model.device,
                    round((time.perf_counter() - load_start) * 1000, 1),
                    request_id,
                )
            inputs, word_lists = self.processor.prepare_forced_aligner_inputs(
                audio=audio, transcript=text, language="English"
            )
            inputs = inputs.to(self.model.device, self.model.dtype)
            with torch.inference_mode():
                output = self.model(**inputs)
            return self.processor.decode_forced_alignment(
                logits=output.logits,
                input_ids=inputs["input_ids"],
                word_lists=word_lists,
                timestamp_token_id=self.model.config.timestamp_token_id,
            )[0]


_MODEL_ALIGNER = None


def align_clip(audio_bytes: bytes, text: str, request_id: str | None = None) -> list[dict]:
    global _MODEL_ALIGNER
    if _MODEL_ALIGNER is None:
        configure_logging()
        _MODEL_ALIGNER = ModelAligner()
    return _MODEL_ALIGNER.align(audio_bytes, text, request_id)


def _request_id(handler) -> str:
    headers = getattr(handler, "headers", None)
    value = headers.get("X-Request-ID") if headers is not None else None
    if value is not None and _REQUEST_ID_RE.fullmatch(value):
        return value
    return uuid.uuid4().hex


def _route(method, path) -> str:
    if method == "GET" and path == "/health":
        return "health"
    if method == "POST" and path == "/align":
        return "align"
    return "unknown"


def _http_reason(code: int) -> str:
    try:
        return HTTPStatus(code).phrase.lower().replace(" ", "_")
    except ValueError:
        return "http_error"


def _bad_input_reason(error: Exception) -> str:
    if isinstance(error, UnicodeError):
        return "invalid_utf8"
    if isinstance(error, KeyError):
        return "missing_field"
    text = str(error)
    if "multipart/form-data" in text:
        return "bad_content_type"
    if "English" in text:
        return "unsupported_language"
    return "invalid_input"


def _terminal_level(status: int, context: dict):
    if status == 200:
        if context.get("word_count") == 0:
            return logging.WARNING, "empty_result"
        return logging.INFO, "ok"
    if status >= 500:
        return logging.ERROR, "worker_error"
    return logging.WARNING, "client_error"


def _kv(key: str, value) -> str:
    if isinstance(value, bool):
        value = "true" if value else "false"
    return f"{key}={value}"


class Aligner:
    """Keep model memory in one worker and stop it after an idle period."""

    def __init__(self, idle_seconds: float | None = None):
        self.idle_seconds = int(os.environ.get("ALIGN_IDLE_SECONDS", "300")) if idle_seconds is None else idle_seconds
        if self.idle_seconds <= 0:
            raise ValueError("ALIGN_IDLE_SECONDS must be positive")
        self.lock = threading.Lock()
        self.pool = None
        self.timer = None
        self.last_used = None
        self.model_loaded = False

    def align(self, audio_bytes: bytes, text: str, request_context: dict | None = None) -> list[dict]:
        request_id = request_context.get("request_id") if request_context is not None else None
        queued_at = time.perf_counter()
        with self.lock:
            locked_at = time.perf_counter()
            if request_context is not None:
                request_context["queue_ms"] = round((locked_at - queued_at) * 1000, 1)
                request_context["cold_start"] = not self.model_loaded
            if self.timer is not None:
                self.timer.cancel()
            try:
                if self.pool is None:
                    LOG.info("event=worker_started")
                    self.pool = ProcessPoolExecutor(max_workers=1, mp_context=get_context("spawn"))
                result = self.pool.submit(align_clip, audio_bytes, text, request_id).result()
                self.model_loaded = True
                return result
            except BrokenProcessPool:
                self._unload(reason="broken")
                raise
            finally:
                self.last_used = time.monotonic()
                if request_context is not None:
                    request_context["worker_ms"] = round((time.perf_counter() - locked_at) * 1000, 1)
                if self.pool is not None:
                    self.timer = threading.Timer(self.idle_seconds, self._expire)
                    self.timer.daemon = True
                    self.timer.start()

    def _expire(self):
        with self.lock:
            if self.last_used is not None and time.monotonic() - self.last_used >= self.idle_seconds:
                self._unload(reason="idle")

    def _unload(self, reason: str = "shutdown"):
        if self.timer is not None:
            self.timer.cancel()
            self.timer = None
        pool, self.pool = self.pool, None
        if pool is not None:
            pool.shutdown(wait=True)
            idle = 0.0 if self.last_used is None else max(0.0, time.monotonic() - self.last_used)
            LOG.info("event=worker_unloaded reason=%s idle_seconds=%.1f", reason, idle)
        self.model_loaded = False

    def close(self):
        with self.lock:
            self._unload(reason="shutdown")


ALIGNER = Aligner()


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        """Suppress the default raw access log; structured events cover requests."""

    def send_error(self, code, message=None, explain=None):
        request_id = _request_id(self)
        self._pending_request_id = request_id
        LOG.warning(
            "event=request method=%s route=%s status=%s request_id=%s outcome=protocol_error reason=%s",
            getattr(self, "command", None),
            _route(getattr(self, "command", None), getattr(self, "path", "")),
            code,
            request_id,
            _http_reason(code),
        )
        super().send_error(code, message, explain)

    def end_headers(self):
        request_id = getattr(self, "_pending_request_id", None)
        if request_id is not None:
            self._pending_request_id = None
            self.send_header("X-Request-ID", request_id)
        super().end_headers()

    def reply(self, status: int, body: dict, context: dict | None = None, level=None, outcome=None, reason=None):
        context = {} if context is None else context
        request_id = context.get("request_id") or _request_id(self)
        started = context.get("start", time.perf_counter())
        data = json.dumps(body).encode()
        try:
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.send_header("X-Request-ID", request_id)
            self.end_headers()
            self.wfile.write(data)
        except (BrokenPipeError, ConnectionResetError):
            level, outcome, reason = logging.WARNING, "client_disconnected", reason or "client_disconnected"
        if level is None:
            level, outcome = _terminal_level(status, context)
        outcome = outcome or "ok"
        fields = [
            ("event", "request"),
            ("method", self.command),
            ("route", _route(self.command, self.path)),
            ("status", status),
            ("request_id", request_id),
            ("duration_ms", round((time.perf_counter() - started) * 1000, 1)),
            ("outcome", outcome),
        ]
        if reason is not None or context.get("reason") is not None:
            fields.append(("reason", reason if reason is not None else context["reason"]))
        for key in ("audio_bytes", "text_chars", "word_count", "queue_ms", "worker_ms", "cold_start", "error_type"):
            if key in context:
                fields.append((key, context[key]))
        LOG.log(level, " ".join(_kv(key, value) for key, value in fields), exc_info=context.get("exc_info"))

    def do_GET(self):
        context = {"request_id": _request_id(self), "start": time.perf_counter()}
        LOG.debug("event=request_started method=GET route=%s request_id=%s", _route("GET", self.path), context["request_id"])
        if self.path == "/health":
            self.reply(200, {"status": "ok", "model_loaded": ALIGNER.model_loaded}, context=context, level=logging.DEBUG)
        else:
            self.reply(404, {"error": "unknown endpoint"}, context=context, reason="not_found")

    def do_POST(self):
        context = {"request_id": _request_id(self), "start": time.perf_counter()}
        LOG.debug("event=request_started method=POST route=%s request_id=%s", _route("POST", self.path), context["request_id"])
        if self.path != "/align":
            self.reply(404, {"error": "unknown endpoint"}, context=context, reason="not_found")
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= MAX_REQUEST_BYTES:
                self.reply(413, {"error": "request must contain at most 4 MiB"}, context=context, reason="request_too_large")
                return
            content_type = self.headers.get("Content-Type", "")
            if not content_type.startswith("multipart/form-data;"):
                raise ValueError("expected multipart/form-data")
            envelope = (
                f"Content-Type: {content_type}\r\nMIME-Version: 1.0\r\n\r\n".encode()
                + self.rfile.read(length)
            )
            message = BytesParser(policy=policy.default).parsebytes(envelope)
            fields = {
                part.get_param("name", header="content-disposition"): part.get_payload(decode=True)
                for part in message.iter_parts()
            }
            if fields.get("language", b"English") != b"English":
                raise ValueError("only English speech alignment is supported")
            audio = fields["file"]
            text = fields["text"].decode("utf-8")
            context["audio_bytes"] = len(audio)
            context["text_chars"] = len(text)
            words = ALIGNER.align(audio, text, context)
            context["word_count"] = len(words)
            self.reply(200, {"words": words}, context=context)
        except (ValueError, KeyError, UnicodeError) as error:
            self.reply(400, {"error": str(error)}, context=context, reason=_bad_input_reason(error))
        except BrokenProcessPool:
            self.reply(503, {"error": "word alignment failed"}, context=context, reason="worker_crashed")
        except Exception as error:
            context["error_type"] = type(error).__name__
            context["exc_info"] = sys.exc_info()
            self.reply(503, {"error": "word alignment failed"}, context=context, reason="internal_error")


def _interrupt(signum, frame):
    """Unwind like Ctrl+C so SIGTERM still runs the cleanup in the finally block."""
    raise KeyboardInterrupt


if __name__ == "__main__":
    configure_logging()
    signal.signal(signal.SIGTERM, _interrupt)
    server = ThreadingHTTPServer(("0.0.0.0", int(os.environ.get("ALIGN_PORT", "8767"))), Handler)
    LOG.info(
        "event=service_started port=%d model=%s threads=%s idle_seconds=%s log_level=%s",
        server.server_address[1],
        MODEL_ID,
        os.environ.get("ALIGN_THREADS", "4"),
        ALIGNER.idle_seconds,
        logging.getLevelName(LOG.getEffectiveLevel()),
    )
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        LOG.info("event=service_interrupted")
    finally:
        LOG.info("event=service_stopping")
        server.server_close()
        ALIGNER.close()
