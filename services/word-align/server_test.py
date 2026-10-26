"""Test the HTTP contract without loading model dependencies."""

import json
import logging
import os
import signal
import socket
import struct
import subprocess
import sys
import threading
import time
import unittest
from concurrent.futures import ThreadPoolExecutor
from concurrent.futures.process import BrokenProcessPool
from unittest.mock import patch
from urllib.error import HTTPError
from urllib.request import Request, urlopen

import server

BOUNDARY = "alignment-test-boundary"


def multipart_body(fields):
    body = b""
    for name, value in fields.items():
        body += (
            f'--{BOUNDARY}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n'.encode()
            + value
            + b"\r\n"
        )
    return body + f"--{BOUNDARY}--\r\n".encode()


def parse_event(message):
    fields = {}
    for part in message.split(" "):
        key, _, value = part.partition("=")
        fields[key] = value
    return fields


class StubAligner:
    model_loaded = False

    def __init__(self):
        self.received = None
        self.failure = None
        self.delay = 0.0

    def align(self, audio_bytes, text, request_context=None):
        self.received = (audio_bytes, text)
        if request_context is not None:
            request_context["queue_ms"] = 1.5
            request_context["worker_ms"] = 2.5
            request_context["cold_start"] = True
        if self.delay:
            time.sleep(self.delay)
        if self.failure is not None:
            raise self.failure
        self.model_loaded = True
        return [{"text": "First", "start_time": 0.2, "end_time": 0.5}]


class RecordingHandler(logging.Handler):
    def __init__(self):
        super().__init__(level=logging.DEBUG)
        self.records = []

    def emit(self, record):
        self.records.append(record)


class ServiceTest(unittest.TestCase):
    def test_upload_health_and_rejected_input(self):
        aligner = StubAligner()
        with patch.object(server, "ALIGNER", aligner):
            httpd = server.ThreadingHTTPServer(("127.0.0.1", 0), server.Handler)
            thread = threading.Thread(target=httpd.serve_forever, daemon=True)
            thread.start()
            base = f"http://127.0.0.1:{httpd.server_port}"
            try:
                with urlopen(base + "/health", timeout=3) as response:
                    self.assertFalse(json.load(response)["model_loaded"])
                boundary = "alignment-test-boundary"
                fields = {"file": b"WAV input", "text": b"First second.", "language": b"English"}
                body = b""
                for name, value in fields.items():
                    body += (
                        f'--{boundary}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n'.encode()
                        + value
                        + b"\r\n"
                    )
                body += f"--{boundary}--\r\n".encode()
                headers = {"Content-Type": f"multipart/form-data; boundary={boundary}"}
                request = Request(base + "/align", data=body, headers=headers)
                with urlopen(request, timeout=3) as response:
                    words = json.load(response)["words"]
                self.assertEqual(aligner.received, (b"WAV input", "First second."))
                self.assertEqual(words[0], {"text": "First", "start_time": 0.2, "end_time": 0.5})
                with urlopen(base + "/health", timeout=3) as response:
                    self.assertTrue(json.load(response)["model_loaded"])
                request = Request(base + "/align", data=body.replace(b"English", b"Chinese"), headers=headers)
                with self.assertRaises(HTTPError) as caught:
                    urlopen(request, timeout=3)
                with caught.exception as response:
                    self.assertEqual(response.code, 400)
                    self.assertIn("English", json.load(response)["error"])
                request = Request(base + "/align", data=b"{}", headers={"Content-Type": "application/json"})
                with self.assertRaises(HTTPError) as caught:
                    urlopen(request, timeout=3)
                with caught.exception as response:
                    self.assertEqual(response.code, 400)
            finally:
                httpd.shutdown()
                httpd.server_close()
                thread.join(timeout=3)


class LoggingTest(unittest.TestCase):
    """Check the structured log contract: one terminal summary, safe fields, quiet health."""

    def setUp(self):
        recorder = RecordingHandler()
        self.addCleanup(server.LOG.removeHandler, recorder)
        self.addCleanup(setattr, server.LOG, "propagate", server.LOG.propagate)
        self.addCleanup(server.LOG.setLevel, server.LOG.level)
        server.LOG.addHandler(recorder)
        server.LOG.setLevel(logging.DEBUG)
        server.LOG.propagate = False
        self.records = recorder.records
        self.aligner = StubAligner()
        patcher = patch.object(server, "ALIGNER", self.aligner)
        patcher.start()
        self.addCleanup(patcher.stop)
        self.httpd = server.ThreadingHTTPServer(("127.0.0.1", 0), server.Handler)
        thread = threading.Thread(target=self.httpd.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(thread.join, 3)
        self.addCleanup(self.httpd.server_close)
        self.addCleanup(self.httpd.shutdown)
        self.base = f"http://127.0.0.1:{self.httpd.server_port}"

    def terminal_events(self):
        return [
            record
            for record in self.records
            if record.levelno >= logging.INFO and record.getMessage().startswith("event=request ")
        ]

    def upload(self, fields=None, request_id=None, content_type=None, data=None):
        if data is None:
            data = multipart_body(
                fields or {"file": b"WAV input", "text": b"First second.", "language": b"English"}
            )
        headers = {"Content-Type": content_type or f"multipart/form-data; boundary={BOUNDARY}"}
        if request_id is not None:
            headers["X-Request-ID"] = request_id
        return Request(self.base + "/align", data=data, headers=headers)

    def test_success_request_logs_one_summary_with_timings(self):
        request = self.upload(request_id="review-success")
        with urlopen(request, timeout=3) as response:
            self.assertEqual(response.headers["X-Request-ID"], "review-success")
            self.assertEqual(json.load(response)["words"][0]["text"], "First")
        summaries = self.terminal_events()
        self.assertEqual(len(summaries), 1)
        fields = parse_event(summaries[0].getMessage())
        self.assertEqual(summaries[0].levelno, logging.INFO)
        self.assertEqual(fields["method"], "POST")
        self.assertEqual(fields["route"], "align")
        self.assertEqual(fields["status"], "200")
        self.assertEqual(fields["request_id"], "review-success")
        self.assertEqual(fields["outcome"], "ok")
        self.assertEqual(fields["audio_bytes"], "9")
        self.assertEqual(fields["text_chars"], "13")
        self.assertEqual(fields["word_count"], "1")
        self.assertEqual(fields["queue_ms"], "1.5")
        self.assertEqual(fields["worker_ms"], "2.5")
        self.assertEqual(fields["cold_start"], "true")
        self.assertGreaterEqual(float(fields["duration_ms"]), 0.0)
        self.assertNotIn("reason", fields)

    def test_empty_result_is_a_warning(self):
        self.aligner.align = lambda audio_bytes, text, request_context=None: []
        with urlopen(self.upload(), timeout=3) as response:
            self.assertEqual(json.load(response)["words"], [])
        summaries = self.terminal_events()
        self.assertEqual(len(summaries), 1)
        self.assertEqual(summaries[0].levelno, logging.WARNING)
        self.assertEqual(parse_event(summaries[0].getMessage())["outcome"], "empty_result")

    def test_health_is_quiet_at_info(self):
        with urlopen(self.base + "/health", timeout=3) as response:
            self.assertEqual(json.load(response)["status"], "ok")
        self.assertEqual([record for record in self.records if record.levelno >= logging.INFO], [])
        summaries = [record for record in self.records if record.getMessage().startswith("event=request ")]
        self.assertEqual(len(summaries), 1)
        self.assertEqual(parse_event(summaries[0].getMessage())["route"], "health")

    def test_unknown_request_id_is_replaced_and_echoed(self):
        request = self.upload(request_id="bad id with spaces")
        with urlopen(request, timeout=3) as response:
            echoed = response.headers["X-Request-ID"]
        self.assertNotEqual(echoed, "bad id with spaces")
        self.assertEqual(parse_event(self.terminal_events()[0].getMessage())["request_id"], echoed)

    def test_request_content_and_query_never_reach_the_log(self):
        secret = "SecretSubtitleLine42"
        request = self.upload(
            fields={"file": b"SECRET-AUDIO-BYTES", "text": secret.encode(), "language": b"English"},
            request_id="bad id with spaces",
        )
        with urlopen(request, timeout=3) as response:
            response.read()
        unknown = Request(self.base + "/align?token=" + secret, method="POST")
        with self.assertRaises(HTTPError) as caught:
            urlopen(unknown, timeout=3)
        with caught.exception:
            formatter = logging.Formatter()
            logged = " ".join(formatter.format(record) for record in self.records)
            self.assertNotIn(secret, logged)
            self.assertNotIn("SECRET-AUDIO-BYTES", logged)
            self.assertNotIn("bad id with spaces", logged)

    def test_protocol_error_returns_request_id_and_logs_once(self):
        for method in ("PATCH", "HEAD"):
            request = Request(self.base + "/align", method=method, headers={"X-Request-ID": f"review-{method}"})
            with self.assertRaises(HTTPError) as caught:
                urlopen(request, timeout=3)
            with caught.exception as response:
                self.assertEqual(response.code, 501)
                self.assertEqual(response.headers["X-Request-ID"], f"review-{method}")
            protocols = [
                record
                for record in self.terminal_events()
                if parse_event(record.getMessage())["request_id"] == f"review-{method}"
            ]
            self.assertEqual(len(protocols), 1)
            fields = parse_event(protocols[0].getMessage())
            self.assertEqual(protocols[0].levelno, logging.WARNING)
            self.assertEqual(fields["method"], method)
            self.assertEqual(fields["route"], "unknown")
            self.assertEqual(fields["status"], "501")
            self.assertEqual(fields["outcome"], "protocol_error")
            self.assertEqual(fields["reason"], "not_implemented")

    def test_client_error_replies_carry_request_id(self):
        cases = [
            (Request(self.base + "/missing", headers={"X-Request-ID": "review-404"}), 404, "not_found"),
            (self.upload(data=b"{}", content_type="application/json", request_id="review-400"), 400, "bad_content_type"),
        ]
        for request, status, reason in cases:
            with self.assertRaises(HTTPError) as caught:
                urlopen(request, timeout=3)
            with caught.exception as response:
                self.assertEqual(response.code, status)
                self.assertEqual(response.headers["X-Request-ID"], request.headers["X-request-id"])
            summaries = [
                record
                for record in self.terminal_events()
                if parse_event(record.getMessage())["request_id"] == request.headers["X-request-id"]
            ]
            self.assertEqual(len(summaries), 1)
            fields = parse_event(summaries[0].getMessage())
            self.assertEqual(summaries[0].levelno, logging.WARNING)
            self.assertEqual(fields["status"], str(status))
            self.assertEqual(fields["reason"], reason)
            self.assertEqual(fields["outcome"], "client_error")

    def test_oversized_request_is_rejected(self):
        client = socket.create_connection(("127.0.0.1", self.httpd.server_port), timeout=3)
        try:
            client.sendall(
                b"POST /align HTTP/1.0\r\nHost: 127.0.0.1\r\n"
                b"Content-Type: multipart/form-data; boundary=x\r\n"
                + f"Content-Length: {server.MAX_REQUEST_BYTES + 1}\r\n".encode()
                + b"X-Request-ID: review-413\r\n\r\n"
            )
            response = client.makefile("rb").read()
        finally:
            client.close()
        self.assertIn(b" 413 ", response.split(b"\r\n", 1)[0])
        self.assertIn(b"X-Request-ID: review-413", response)
        fields = parse_event(self.terminal_events()[0].getMessage())
        self.assertEqual(fields["status"], "413")
        self.assertEqual(fields["reason"], "request_too_large")
        self.assertEqual(fields["request_id"], "review-413")
        self.assertNotIn("audio_bytes", fields)

    def test_bad_input_reason_mapping(self):
        cases = [
            ({"file": b"WAV input", "language": b"English"}, "missing_field"),
            ({"file": b"WAV input", "text": b"\xff\xfe", "language": b"English"}, "invalid_utf8"),
            ({"file": b"WAV input", "text": b"First.", "language": b"Chinese"}, "unsupported_language"),
        ]
        for fields, reason in cases:
            with self.assertRaises(HTTPError) as caught:
                urlopen(self.upload(fields=fields), timeout=3)
            with caught.exception as response:
                self.assertEqual(response.code, 400)
            summaries = self.terminal_events()
            self.assertEqual(len(summaries), 1)
            self.assertEqual(parse_event(summaries[0].getMessage())["reason"], reason)
            self.records.clear()

    def test_failed_worker_keeps_timings_and_error_type(self):
        cases = [(BrokenProcessPool("worker stopped"), "worker_crashed", "ERROR", None), (RuntimeError("boom"), "internal_error", "ERROR", "RuntimeError")]
        for failure, reason, level, error_type in cases:
            self.records.clear()
            self.aligner.failure = failure
            with self.assertRaises(HTTPError) as caught:
                urlopen(self.upload(request_id="review-failed"), timeout=3)
            with caught.exception as response:
                self.assertEqual(response.code, 503)
            summaries = self.terminal_events()
            self.assertEqual(len(summaries), 1)
            fields = parse_event(summaries[0].getMessage())
            self.assertEqual(summaries[0].levelname, level)
            self.assertEqual(fields["status"], "503")
            self.assertEqual(fields["reason"], reason)
            self.assertEqual(fields["queue_ms"], "1.5")
            self.assertEqual(fields["worker_ms"], "2.5")
            self.assertEqual(fields["cold_start"], "true")
            self.assertEqual(fields.get("error_type"), error_type)
        self.aligner.failure = None

    def test_client_disconnect_logs_one_warning(self):
        self.aligner.delay = 0.5
        body = multipart_body({"file": b"WAV input", "text": b"First second.", "language": b"English"})
        raw = (
            "POST /align HTTP/1.0\r\nHost: 127.0.0.1\r\n"
            f"Content-Type: multipart/form-data; boundary={BOUNDARY}\r\n"
            f"Content-Length: {len(body)}\r\nX-Request-ID: dropped-client\r\n\r\n"
        ).encode() + body
        client = socket.create_connection(("127.0.0.1", self.httpd.server_port), timeout=3)
        try:
            client.sendall(raw)
            time.sleep(0.05)
            client.setsockopt(socket.SOL_SOCKET, socket.SO_LINGER, struct.pack("ii", 1, 0))
        finally:
            client.close()
        summaries = []
        deadline = time.monotonic() + 3
        while time.monotonic() < deadline:
            summaries = [
                record
                for record in self.terminal_events()
                if parse_event(record.getMessage())["request_id"] == "dropped-client"
            ]
            if summaries:
                break
            time.sleep(0.05)
        self.assertEqual(len(summaries), 1)
        self.assertEqual(summaries[0].levelno, logging.WARNING)
        fields = parse_event(summaries[0].getMessage())
        self.assertEqual(fields["outcome"], "client_disconnected")
        self.assertEqual(fields["reason"], "client_disconnected")
        self.assertEqual(fields["status"], "200")

    def test_configure_logging_is_idempotent_and_quiets_libraries(self):
        with patch.dict(server.os.environ, {"ALIGN_LOG_LEVEL": "debug"}):
            for handler in list(server.LOG.handlers):
                server.LOG.removeHandler(handler)
            try:
                server.configure_logging()
                server.configure_logging()
                self.assertEqual(len(server.LOG.handlers), 1)
                self.assertEqual(server.LOG.level, logging.DEBUG)
                self.assertFalse(server.LOG.propagate)
                self.assertEqual(logging.getLogger("transformers").level, logging.WARNING)
                self.assertEqual(logging.getLogger("torch").level, logging.WARNING)
            finally:
                for handler in list(server.LOG.handlers):
                    server.LOG.removeHandler(handler)


class StubPool(ThreadPoolExecutor):
    def __init__(self):
        super().__init__(max_workers=1)
        self.stopped = threading.Event()

    def shutdown(self, wait=True):
        super().shutdown(wait=wait)
        self.stopped.set()


class ModelLifecycleTest(unittest.TestCase):
    def setUp(self):
        self.pools = []
        factory = patch.object(server, "ProcessPoolExecutor", side_effect=self.new_pool)
        factory.start()
        self.addCleanup(factory.stop)
        work = patch.object(server, "align_clip", return_value=[{"text": "First"}])
        self.work = work.start()
        self.addCleanup(work.stop)

    def new_pool(self, **kwargs):
        self.assertEqual(kwargs["max_workers"], 1)
        self.assertEqual(kwargs["mp_context"].get_start_method(), "spawn")
        pool = StubPool()
        self.pools.append(pool)
        return pool

    def new_aligner(self, idle_seconds=0.05):
        aligner = server.Aligner(idle_seconds=idle_seconds)
        self.addCleanup(aligner.close)
        return aligner

    def test_lazy_load_reuse_idle_release_and_reload(self):
        aligner = self.new_aligner(idle_seconds=0.15)
        self.assertFalse(aligner.model_loaded)
        self.assertEqual(self.pools, [])
        self.assertEqual(aligner.align(b"audio", "First"), [{"text": "First"}])
        aligner.align(b"more audio", "First")
        self.assertEqual(len(self.pools), 1)
        self.assertTrue(aligner.model_loaded)
        self.assertTrue(self.pools[0].stopped.wait(timeout=3))
        with aligner.lock:
            self.assertFalse(aligner.model_loaded)
        aligner.align(b"audio again", "First")
        self.assertEqual(len(self.pools), 2)
        self.assertTrue(aligner.model_loaded)

    @patch.object(server.time, "monotonic", return_value=100.0)
    def test_idle_callback_does_not_stop_an_active_request(self, clock):
        aligner = self.new_aligner()
        aligner.align(b"audio", "First")
        entered, release = threading.Event(), threading.Event()
        result = []

        def slow_work(*args):
            entered.set()
            if not release.wait(timeout=3):
                raise RuntimeError("test did not release the request")
            return [{"text": "First"}]

        self.work.side_effect = slow_work
        request = threading.Thread(target=lambda: result.append(aligner.align(b"audio", "First")))
        request.start()
        self.assertTrue(entered.wait(timeout=3))
        clock.return_value = 200.0
        stale_timer = threading.Thread(target=aligner._expire)
        stale_timer.start()
        try:
            self.assertFalse(self.pools[0].stopped.wait(timeout=0.1))
        finally:
            release.set()
            request.join(timeout=3)
            stale_timer.join(timeout=3)
        self.assertFalse(request.is_alive())
        self.assertFalse(stale_timer.is_alive())
        self.assertEqual(result, [[{"text": "First"}]])
        self.assertTrue(aligner.model_loaded)
        self.assertEqual(len(self.pools), 1)

    def test_second_request_waits_on_the_lock(self):
        aligner = self.new_aligner(idle_seconds=5)
        entered, release = threading.Event(), threading.Event()

        def slow_work(*args):
            entered.set()
            if not release.wait(timeout=3):
                raise RuntimeError("test did not release the request")
            return [{"text": "First"}]

        self.work.side_effect = slow_work
        first, second = {"request_id": "first"}, {"request_id": "second"}
        threads = [
            threading.Thread(target=lambda: aligner.align(b"audio", "First", first)),
            threading.Thread(target=lambda: aligner.align(b"audio", "First", second)),
        ]
        threads[0].start()
        self.assertTrue(entered.wait(timeout=3))
        threads[1].start()
        time.sleep(0.2)
        release.set()
        for thread in threads:
            thread.join(timeout=3)
            self.assertFalse(thread.is_alive())
        self.assertTrue(first["cold_start"])
        self.assertFalse(second["cold_start"])
        self.assertGreater(second["queue_ms"], 0.0)
        self.assertGreaterEqual(first["queue_ms"], 0.0)
        self.assertGreater(first["worker_ms"], 0.0)
        self.assertGreater(second["worker_ms"], 0.0)
        self.assertEqual(len(self.pools), 1)

    def test_failed_worker_is_replaced_on_next_request(self):
        aligner = self.new_aligner()
        self.work.side_effect = BrokenProcessPool("worker stopped")
        with self.assertRaises(BrokenProcessPool):
            aligner.align(b"audio", "First")
        self.assertFalse(aligner.model_loaded)
        self.assertTrue(self.pools[0].stopped.is_set())
        self.work.side_effect = None
        aligner.align(b"audio", "First")
        self.assertEqual(len(self.pools), 2)
        self.assertTrue(aligner.model_loaded)

    @patch.object(server.time, "monotonic", return_value=100.0)
    def test_first_worker_crash_reports_zero_idle(self, clock):
        aligner = self.new_aligner()
        self.work.side_effect = BrokenProcessPool("worker stopped")
        with self.assertLogs(server.LOG, level="INFO") as captured:
            with self.assertRaises(BrokenProcessPool):
                aligner.align(b"audio", "First")
        unloaded = [line for line in captured.output if "event=worker_unloaded" in line]
        self.assertEqual(len(unloaded), 1)
        self.assertIn("reason=broken", unloaded[0])
        self.assertIn("idle_seconds=0.0", unloaded[0])

    def test_idle_unload_reports_elapsed_idle(self):
        aligner = self.new_aligner(idle_seconds=0.1)
        with self.assertLogs(server.LOG, level="INFO") as captured:
            aligner.align(b"audio", "First")
            self.assertTrue(self.pools[0].stopped.wait(timeout=3))
            with aligner.lock:
                pass
        unloaded = [line for line in captured.output if "event=worker_unloaded reason=idle" in line]
        self.assertEqual(len(unloaded), 1)
        self.assertGreater(float(unloaded[0].split("idle_seconds=")[1]), 0.05)

    def test_idle_timeout_configuration(self):
        with patch.dict(server.os.environ, {"ALIGN_IDLE_SECONDS": "120"}):
            self.assertEqual(self.new_aligner(idle_seconds=None).idle_seconds, 120)
        with self.assertRaises(ValueError):
            server.Aligner(idle_seconds=0)


class ShutdownSignalTest(unittest.TestCase):
    """A SIGTERM (for example `docker stop`) must still run the finally cleanup."""

    def test_sigterm_stops_the_service_with_cleanup_logs(self):
        script = os.path.join(os.path.dirname(os.path.abspath(__file__)), "server.py")
        process = subprocess.Popen(
            [sys.executable, "-B", script],
            env=dict(os.environ, ALIGN_PORT="0", ALIGN_LOG_LEVEL="INFO"),
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        self.addCleanup(process.kill)
        time.sleep(1.0)
        self.assertIsNone(process.poll(), "service exited before SIGTERM")
        process.send_signal(signal.SIGTERM)
        _, stderr = process.communicate(timeout=10)
        self.assertEqual(process.returncode, 0)
        self.assertIn("event=service_started", stderr)
        self.assertIn("event=service_interrupted", stderr)
        self.assertIn("event=service_stopping", stderr)


if __name__ == "__main__":
    unittest.main()
