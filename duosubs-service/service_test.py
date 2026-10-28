"""Test the DuoSubs service contract with stubbed fastapi/uvicorn/duosubs.

The real dependencies are not installed in unit-test environments (and
``duosubs`` downloads large models), so the modules are stubbed before import.
"""

import asyncio
import io
import json
import logging
import sys
import tempfile
import types
import unittest
import zipfile
from pathlib import Path
from types import SimpleNamespace


class _StubApp:
    def __init__(self, **kwargs):
        self.kwargs = kwargs

    def get(self, path, **kwargs):
        return lambda function: function

    def post(self, path, **kwargs):
        return lambda function: function

    def middleware(self, kind):
        return lambda function: function


def _install_stubs():
    if "fastapi" not in sys.modules:
        sys.modules["fastapi"] = types.ModuleType("fastapi")
        sys.modules["fastapi"].FastAPI = _StubApp
        sys.modules["fastapi"].Request = object
        responses = types.ModuleType("fastapi.responses")
        responses.JSONResponse = lambda status_code, content: SimpleNamespace(
            status_code=status_code, headers={}, content=content
        )
        sys.modules["fastapi.responses"] = responses
        sys.modules["fastapi"].responses = responses
    if "uvicorn" not in sys.modules:
        sys.modules["uvicorn"] = types.ModuleType("uvicorn")
        sys.modules["uvicorn"].run = lambda *args, **kwargs: None
    if "duosubs" not in sys.modules:
        module = types.ModuleType("duosubs")
        module.MergeArgs = lambda **kwargs: kwargs
        module.run_merge_pipeline = lambda args, progress: None
        sys.modules["duosubs"] = module


_install_stubs()
sys.path.insert(0, str(Path(__file__).resolve().parent))
import service  # noqa: E402

sys.path.insert(0, str(Path(service.__file__).resolve().parent))
import logfmt  # noqa: E402

logging.getLogger().handlers.clear()  # keep the display stream quiet in tests


class RecordingHandler(logging.Handler):
    def __init__(self):
        super().__init__(level=logging.DEBUG)
        self.records = []

    def emit(self, record):
        self.records.append(record)


def record_fields(record):
    return dict(getattr(record, "fields", None) or {})


def render_record(record):
    return logfmt.format_line(
        record.levelno,
        getattr(record, "event", "log_message"),
        record.getMessage(),
        record_fields(record),
        component=getattr(record, "component", None),
        service=getattr(record, "service", None),
    )


class ServiceTest(unittest.TestCase):
    def setUp(self):
        self.recorder = RecordingHandler()
        root = logging.getLogger()
        root.addHandler(self.recorder)
        root.setLevel(logging.DEBUG)
        self.addCleanup(root.removeHandler, self.recorder)
        self.records = self.recorder.records
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.tmp = Path(self._tmp.name)

    def enable_debug(self):
        """Re-configure at DEBUG; configure() replaces root handlers."""
        logfmt.configure("duosubs", component="service", level="DEBUG")
        root = logging.getLogger()
        for handler in list(root.handlers):
            if handler is not self.recorder:
                root.removeHandler(handler)
        root.addHandler(self.recorder)
        self.addCleanup(lambda: root.removeHandler(self.recorder))

    def events(self, name):
        return [record for record in self.records if getattr(record, "event", None) == name]

    def write_inputs(self):
        primary = self.tmp / "zh.srt"
        secondary = self.tmp / "en.srt"
        primary.write_text("1\n00:00:01,000 --> 00:00:02,000\n你好\n")
        secondary.write_text("1\n00:00:01,000 --> 00:00:02,000\nhello\n")
        return primary, secondary

    def request(self, **overrides):
        payload = {
            "primary_path": str(self.tmp / "zh.srt"),
            "secondary_path": str(self.tmp / "en.srt"),
            "output_dir": str(self.tmp / "out"),
            "container_prefix": "/data",
            "host_prefix": str(self.tmp),
        }
        payload.update(overrides)
        return service.MergeRequest(**payload)

    def http_request(self, request_id="merge-req-1"):
        return SimpleNamespace(state=SimpleNamespace(request_id=request_id))

    def test_translate_path(self):
        self.assertEqual(service.translate_path("/data/a/b.srt", "/data", "/host"), "/host/a/b.srt")
        self.assertEqual(service.translate_path("/other/b.srt", "/data", "/host"), "/other/b.srt")

    def test_collect_progress_is_bounded(self):
        progress, collect = service.collect_progress(limit=4)
        for i in range(10):
            collect(f"step {i}")
        self.assertLessEqual(len(progress), 4)

        # Total bytes are bounded too, and one huge line is cut.
        progress, collect = service.collect_progress(limit=100, byte_limit=64)
        for i in range(10):
            collect("x" * 50)
        self.assertLessEqual(sum(len(item.encode()) for item in progress), 64)

        progress, collect = service.collect_progress(byte_limit=32)
        collect("y" * 1000)
        self.assertLessEqual(len(progress[0].encode()), 32)

    def test_missing_primary_logs_warning_without_path(self):
        result = asyncio.run(service.merge_subtitles(self.request(primary_path="/nope/zh.srt"), self.http_request()))
        self.assertFalse(result.success)
        self.assertIn("Primary subtitle not found", result.error)

        events = self.events("merge_failed")
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0].levelno, logging.WARNING)
        fields = record_fields(events[0])
        self.assertEqual(fields["reason"], "primary_not_found")
        self.assertEqual(fields["request_id"], "merge-req-1")

    def test_successful_merge_logs_start_and_completion(self):
        self.write_inputs()
        primary = self.tmp / "zh.srt"
        output_dir = self.tmp / "out"
        output_dir.mkdir()

        def fake_pipeline(args, progress):
            progress("loading model")
            progress("aligning")
            zip_path = output_dir / "zh.zip"
            with zipfile.ZipFile(zip_path, "w") as archive:
                archive.writestr("zh_combined.ass", "[Script Info]\n")
            return None

        original = service.run_merge_pipeline
        service.run_merge_pipeline = fake_pipeline
        self.addCleanup(setattr, service, "run_merge_pipeline", original)

        result = asyncio.run(service.merge_subtitles(self.request(), self.http_request()))
        self.assertTrue(result.success, result.error)
        self.assertTrue(result.output_path.endswith("zh_combined.ass"))

        started = self.events("merge_started")
        completed = self.events("merge_completed")
        self.assertEqual(len(started), 1)
        self.assertEqual(len(completed), 1)
        self.assertEqual(started[0].levelno, logging.INFO)
        self.assertEqual(record_fields(completed[0])["request_id"], "merge-req-1")
        self.assertGreaterEqual(float(record_fields(completed[0])["duration_ms"]), 0.0)

        # Progress and paths are bounded DEBUG detail, not raw INFO output.
        rendered_info = " ".join(
            render_record(record) for record in self.records if record.levelno >= logging.INFO
        )
        self.assertNotIn("loading model", rendered_info)
        self.assertNotIn(str(primary), rendered_info)

    def test_merge_failure_keeps_exception_type_and_redacts_detail(self):
        self.enable_debug()
        self.write_inputs()

        def failing_pipeline(args, progress):
            raise RuntimeError("provider said api_key=FAKESECRETVALUE")

        original = service.run_merge_pipeline
        service.run_merge_pipeline = failing_pipeline
        self.addCleanup(setattr, service, "run_merge_pipeline", original)

        result = asyncio.run(service.merge_subtitles(self.request(), self.http_request()))
        self.assertFalse(result.success)
        self.assertIn("Merge failed", result.error)

        failures = self.events("merge_failed")
        self.assertEqual(len(failures), 1)
        self.assertEqual(failures[0].levelno, logging.ERROR)
        self.assertEqual(record_fields(failures[0])["error"], "RuntimeError")
        self.assertEqual(record_fields(failures[0])["reason"], "merge_error")

        # The DEBUG detail is redacted: the fake credential never reaches the sink.
        rendered = " ".join(render_record(record) for record in self.records)
        self.assertNotIn("FAKESECRETVALUE", rendered)
        self.assertIn("api_key=***", rendered)

    def test_request_summary_levels_and_health(self):
        self.enable_debug()

        async def call_next(request):
            return SimpleNamespace(status_code=200, headers={})

        request = SimpleNamespace(
            headers={"X-Request-ID": "bad id!"},
            method="POST",
            url=SimpleNamespace(path="/merge"),
            state=SimpleNamespace(),
        )
        response = asyncio.run(service.log_requests(request, call_next))
        self.assertTrue(logfmt.request_id(response.headers["X-Request-ID"]) == response.headers["X-Request-ID"])
        self.assertNotEqual(response.headers["X-Request-ID"], "bad id!")
        summary = self.events("request_completed")
        self.assertEqual(len(summary), 1)
        self.assertEqual(summary[0].levelno, logging.INFO)

        # An uncaught exception yields exactly one ERROR event (no duplicate summary).
        async def failing_next(request):
            raise RuntimeError("boom")

        broken = SimpleNamespace(
            headers={"X-Request-ID": "boom-1"},
            method="GET",
            url=SimpleNamespace(path="/boom"),
            state=SimpleNamespace(),
        )
        self.records.clear()
        response = asyncio.run(service.log_requests(broken, failing_next))
        failures = self.events("request_completed")
        self.assertEqual(len(failures), 1)
        self.assertEqual(failures[0].levelno, logging.ERROR)
        self.assertEqual(record_fields(failures[0])["status"], 500)
        # The record carries the traceback; the shared formatter turns it into
        # a bounded error/stack field (covered by logfmt_test.py).
        self.assertIsNotNone(failures[0].exc_info)
        self.assertEqual(failures[0].exc_info[0].__name__, "RuntimeError")
        self.assertEqual(response.status_code, 500)
        self.assertEqual(response.headers["X-Request-ID"], "boom-1")

        health = SimpleNamespace(
            headers={},
            method="GET",
            url=SimpleNamespace(path="/health"),
            state=SimpleNamespace(),
        )
        asyncio.run(service.log_requests(health, call_next))
        health_summary = [
            record
            for record in self.events("request_completed")
            if record_fields(record)["route"] == "/health"
        ]
        self.assertEqual(len(health_summary), 1)
        self.assertEqual(health_summary[0].levelno, logging.DEBUG)


if __name__ == "__main__":
    unittest.main()
