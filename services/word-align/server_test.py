"""Test the HTTP contract without loading model dependencies."""

import json
import threading
import unittest
from concurrent.futures import ThreadPoolExecutor
from concurrent.futures.process import BrokenProcessPool
from unittest.mock import patch
from urllib.error import HTTPError
from urllib.request import Request, urlopen

import server


class StubAligner:
    model_loaded = False

    def __init__(self):
        self.received = None

    def align(self, audio_bytes, text):
        self.received = (audio_bytes, text)
        self.model_loaded = True
        return [{"text": "First", "start_time": 0.2, "end_time": 0.5}]


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

    def test_idle_timeout_configuration(self):
        with patch.dict(server.os.environ, {"ALIGN_IDLE_SECONDS": "120"}):
            self.assertEqual(self.new_aligner(idle_seconds=None).idle_seconds, 120)
        with self.assertRaises(ValueError):
            server.Aligner(idle_seconds=0)


if __name__ == "__main__":
    unittest.main()
