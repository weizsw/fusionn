"""HTTP word alignment for English source subtitles.

Run this service beside the Go container. The model loads on the first request.
Only audio clips and their existing transcripts enter the model.
"""

from __future__ import annotations

import io
import json
import logging
import os
import threading
import time
from concurrent.futures import ProcessPoolExecutor
from concurrent.futures.process import BrokenProcessPool
from multiprocessing import get_context
from email import policy
from email.parser import BytesParser
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MAX_REQUEST_BYTES = 4 * 1024 * 1024
MODEL_ID = os.environ.get("ALIGN_MODEL", "Qwen/Qwen3-ForcedAligner-0.6B-hf")
LOG = logging.getLogger("word-align")


class ModelAligner:
    def __init__(self):
        self.model = None
        self.processor = None
        self.lock = threading.Lock()

    def align(self, audio_bytes: bytes, text: str) -> list[dict]:
        import soundfile as sf
        import torch
        from transformers import AutoModelForTokenClassification, AutoProcessor

        audio, sample_rate = sf.read(io.BytesIO(audio_bytes), dtype="float32")
        if sample_rate != 16000 or audio.ndim != 1 or not 0 < len(audio) <= 31 * 16000:
            raise ValueError("audio must be mono 16 kHz WAV and at most 31 seconds")
        if not text.strip() or len(text) > 4096:
            raise ValueError("text must contain 1 to 4096 characters")
        with self.lock:
            if self.model is None:
                torch.set_num_threads(int(os.environ.get("ALIGN_THREADS", "4")))
                LOG.info("Loading %s on CPU", MODEL_ID)
                processor = AutoProcessor.from_pretrained(MODEL_ID)
                model = AutoModelForTokenClassification.from_pretrained(
                    MODEL_ID, dtype=torch.float32, attn_implementation="sdpa"
                ).eval()
                self.processor, self.model = processor, model
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


def align_clip(audio_bytes: bytes, text: str) -> list[dict]:
    global _MODEL_ALIGNER
    if _MODEL_ALIGNER is None:
        logging.basicConfig(level=logging.INFO)
        _MODEL_ALIGNER = ModelAligner()
    return _MODEL_ALIGNER.align(audio_bytes, text)


class Aligner:
    """Keep model memory in one worker and stop it after an idle period."""

    def __init__(self, idle_seconds: float | None = None):
        self.idle_seconds = int(os.environ.get("ALIGN_IDLE_SECONDS", "300")) if idle_seconds is None else idle_seconds
        if self.idle_seconds <= 0:
            raise ValueError("ALIGN_IDLE_SECONDS must be positive")
        self.lock = threading.Lock()
        self.pool = None
        self.timer = None
        self.last_used = 0.0
        self.model_loaded = False

    def align(self, audio_bytes: bytes, text: str) -> list[dict]:
        with self.lock:
            if self.timer is not None:
                self.timer.cancel()
            try:
                if self.pool is None:
                    self.pool = ProcessPoolExecutor(max_workers=1, mp_context=get_context("spawn"))
                result = self.pool.submit(align_clip, audio_bytes, text).result()
                self.model_loaded = True
                return result
            except BrokenProcessPool:
                self._unload()
                raise
            finally:
                self.last_used = time.monotonic()
                if self.pool is not None:
                    self.timer = threading.Timer(self.idle_seconds, self._expire)
                    self.timer.daemon = True
                    self.timer.start()

    def _expire(self):
        with self.lock:
            if time.monotonic() - self.last_used >= self.idle_seconds:
                LOG.info("Stopping idle model worker")
                self._unload()

    def _unload(self):
        if self.timer is not None:
            self.timer.cancel()
            self.timer = None
        pool, self.pool = self.pool, None
        if pool is not None:
            pool.shutdown(wait=True)
        self.model_loaded = False

    def close(self):
        with self.lock:
            self._unload()


ALIGNER = Aligner()


class Handler(BaseHTTPRequestHandler):
    def reply(self, status: int, body: dict):
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        try:
            self.wfile.write(data)
        except (BrokenPipeError, ConnectionResetError):
            pass

    def do_GET(self):
        if self.path == "/health":
            self.reply(200, {"status": "ok", "model_loaded": ALIGNER.model_loaded})
        else:
            self.reply(404, {"error": "unknown endpoint"})

    def do_POST(self):
        if self.path != "/align":
            self.reply(404, {"error": "unknown endpoint"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= MAX_REQUEST_BYTES:
                self.reply(413, {"error": "request must contain at most 4 MiB"})
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
            words = ALIGNER.align(fields["file"], fields["text"].decode("utf-8"))
            self.reply(200, {"words": words})
        except (ValueError, KeyError, UnicodeError) as error:
            self.reply(400, {"error": str(error)})
        except Exception:
            LOG.exception("Word alignment failed")
            self.reply(503, {"error": "word alignment failed"})


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    server = ThreadingHTTPServer(("0.0.0.0", 8767), Handler)
    LOG.info("Listening on port 8767")
    try:
        server.serve_forever()
    finally:
        server.server_close()
        ALIGNER.close()
