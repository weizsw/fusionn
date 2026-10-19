"""Offline guardrails and renderer invariants; no model calls or media required."""
import argparse
from dataclasses import asdict
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest

import pysubs2

spec = importlib.util.spec_from_file_location("subtitle_merge_experiment", Path(__file__).with_name("merge.py"))
m = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = m
spec.loader.exec_module(m)
compare_spec = importlib.util.spec_from_file_location("subtitle_compare_experiment", Path(__file__).with_name("compare.py"))
comparison = importlib.util.module_from_spec(compare_spec)
compare_spec.loader.exec_module(comparison)


def cue(i, start=1000, end=3000, text="local text", **kw):
    return m.Cue(i, start, end, start, end, text, text, text, "Default", 2, None, False, False, (), **kw)


def bundle(es, zs):
    b = {"version": 1, "en": [asdict(c) for c in es], "zh": [asdict(c) for c in zs]}
    b["fingerprint"] = m.digest(b)
    return b


def row(es, zs, layer="bottom"):
    return {"en_ids": es, "zh_ids": zs, "layer": layer}


def render_args(**kw):
    return argparse.Namespace(en_chars=65, zh_chars=25, en_gap_ms=400, zh_gap_ms=500,
                              span_ms=5500, fill_gap_ms=500, zh_font="PingFang SC", en_font="Helvetica Neue", **kw)


class Guardrails(unittest.TestCase):
    def setUp(self):
        self.b = bundle([cue(1), cue(2, 3100, 5000)], [cue(1), cue(2, 3100, 5000)])
        self.w = {l: self.b[l] for l in ("en", "zh")}

    def validate(self, rows):
        return m.validate_response({"alignments": rows}, self.w, 8000)

    def test_forbid_model_text_and_bad_envelope(self):
        with self.assertRaises(ValueError):
            m.validate_response({"alignments": [], "text": "invented"}, self.w, 8000)
        good, bad = self.validate([{**row([1], [1]), "text": "invented"}])
        self.assertFalse(good)
        self.assertEqual(bad[0]["reason"], "row_structure")

    def test_bad_ids_empty_and_types(self):
        for r in [row([True], [1]), row([1.0], [1]), row(["1"], [1]), row([99], [1]),
                  row([1, 1], [1]), row([], []), row(None, [1]), row([1], [1], "middle")]:
            with self.subTest(r=r):
                self.assertFalse(self.validate([r])[0])

    def test_duplicate_usage_quarantines_both_rows(self):
        good, bad = self.validate([row([1], [1]), row([1], [2])])
        self.assertFalse(good)
        self.assertEqual(len(bad), 2)

    def test_far_outlier_not_hidden_by_group_envelope(self):
        self.w["zh"][1]["start"] = 40000
        self.w["zh"][1]["end"] = 41000
        good, bad = self.validate([row([1, 2], [1, 2])])
        self.assertFalse(good)
        self.assertIn(bad[0]["reason"], ("too_far", "group_span_over_30s"))

    def test_window_overlap_and_owner_conflict(self):
        b = bundle([cue(1, 208000, 209000), cue(2, 211000, 212000)],
                   [cue(1, 208000, 212000)])
        ws = m.windows(b, 210000, 15000)
        self.assertEqual(len(ws), 2)
        self.assertEqual([c["id"] for c in ws[1]["en"]], [1, 2])
        batches = [{"index": 0, "valid": [row([1, 2], [1])], "rejected": [], "missing": {}},
                   {"index": 1, "valid": [row([1], [1]), row([2], [])], "rejected": [], "missing": {}}]
        groups, audit = m.reconcile(b, batches, 210000)
        self.assertTrue(audit["conflicts"])
        self.assertTrue(all(g["origin"] == "restored" for g in groups))
        self.assertEqual(audit["restored"], {"en": [1, 2], "zh": [1]})

    def test_identical_context_is_deduplicated(self):
        batches = [{"index": i, "valid": [row([1], [1])], "rejected": [], "missing": {}} for i in (0, 1)]
        groups, audit = m.reconcile(self.b, batches, 210000)
        self.assertEqual(sum(g["origin"] == "model" for g in groups), 1)
        self.assertEqual(audit["context_duplicates"], 1)

    def test_modified_source_rejected(self):
        with tempfile.TemporaryDirectory() as d:
            p = Path(d) / "sources.json"
            b = self.b.copy()
            b["en"][0]["raw"] = "changed"
            m.save(p, b)
            with self.assertRaises(ValueError):
                m.source_bundle(p)

    def test_native_top_and_raw_newline_trace(self):
        with tempfile.TemporaryDirectory() as d:
            p = Path(d) / "source.srt"
            p.write_text("1\n00:00:01,000 --> 00:00:03,000\n{\\an8}- Hello\n- Goodbye\n", encoding="utf-8")
            c = m.read_track(p)[0]
            self.assertTrue(c.top)
            self.assertTrue(c.multi_speaker)
            self.assertIn("whitespace_flattened", c.changes)
            self.assertEqual(c.display, "- Hello   - Goodbye")
            self.assertIn(r"{\an8}", c.raw)

    def test_illegal_source_time_fails(self):
        with tempfile.TemporaryDirectory() as d:
            p = Path(d) / "bad.srt"
            p.write_text("1\n00:00:03,000 --> 00:00:01,000\nbad\n", encoding="utf-8")
            with self.assertRaises(ValueError):
                m.read_track(p)


class Timing(unittest.TestCase):
    def blocks(self, es, zs, **kw):
        return m.build_blocks(row([c.id for c in es], [c.id for c in zs]),
                              {c.id: c for c in es}, {c.id: c for c in zs}, 0, render_args(**kw))

    def test_one_to_one_english_anchor_not_union(self):
        b = self.blocks([cue(1, 1000, 3000)], [cue(1, 500, 4000)])[0]
        self.assertEqual((b.start, b.end), (1000, 3000))

    def test_short_many_to_one_joins_and_long_slices_fill_small_gap(self):
        zs = [cue(1, 1000, 5000, "中文")]
        self.assertEqual(len(self.blocks([cue(1), cue(2, 3100, 5000)], zs)), 1)
        es = [cue(1, text="a " * 40), cue(2, 3100, 5000)]
        bs = self.blocks(es, zs)
        self.assertEqual(len(bs), 2)
        self.assertEqual(bs[0].end, bs[1].start)
        self.assertEqual(bs[0].zh_ids, bs[1].zh_ids)

    def test_speaker_dash_prevents_join(self):
        bs = self.blocks([cue(1, text="Hello"), cue(2, 3100, 5000, "- Goodbye")], [cue(1)])
        self.assertEqual(len(bs), 2)

    def test_big_gap_is_not_filled(self):
        bs = self.blocks([cue(1), cue(2, 4000, 5000)], [cue(1)])
        self.assertEqual(bs[0].end, 3000)

    def test_short_overlap_is_never_trimmed(self):
        bs = self.blocks([cue(1), cue(2, 2900, 5000)], [cue(1)])
        self.assertEqual(bs[0].end, 3000)
        self.assertEqual(bs[1].start, 2900)

    def test_long_one_to_many_inner_cut_and_exact_outer_bounds(self):
        bs = self.blocks([cue(1, 1000, 7000)],
                         [cue(1, 500, 3500, "甲" * 20), cue(2, 4000, 8000, "乙" * 20)])
        self.assertEqual([(b.start, b.end) for b in bs], [(1000, 4000), (4000, 7000)])
        self.assertEqual([b.zh_ids for b in bs], [[1], [2]])

    def test_invalid_internal_cut_preserves_all_text_and_flags(self):
        bs = self.blocks([cue(1, 1000, 2000)], [cue(1, text="甲" * 20), cue(2, 4000, 5000, "乙" * 20)])
        self.assertEqual(bs[0].zh_ids, [1, 2])
        self.assertIn("review", bs[0].strategy)

    def test_many_to_many_retains_every_id_and_flags_heuristic(self):
        bs = self.blocks([cue(1, text="a " * 40), cue(2, 3100, 6000, "b " * 40)],
                         [cue(1, text="甲" * 20), cue(2, 4000, 6000, "乙" * 20)])
        self.assertEqual({i for b in bs for i in b.zh_ids}, {1, 2})
        self.assertTrue(all("review" in b.strategy for b in bs))

    def test_render_roundtrip_overlap_sdh_and_one_sided(self):
        es = [cue(1, 1000, 3000, "[phone ringing]"), cue(2, 2900, 5000, "Speech")]
        zs = [cue(1, 2900, 5000, "台词"), cue(2, 3000, 6000, "路牌")]
        b = bundle(es, zs)
        groups = [row([1], []), row([2], [1]), row([], [2], "top")]
        mapping = {"source_fingerprint": b["fingerprint"], "parameters": {"max_distance_ms": 8000}, "groups": groups}
        mapping["fingerprint"] = m.digest(mapping)
        with tempfile.TemporaryDirectory() as d:
            d = Path(d)
            m.save(d / "sources.json", b)
            m.save(d / "mapping.json", mapping)
            m.render(render_args(sources=d / "sources.json", mapping=d / "mapping.json", out=d / "render"))
            ass = pysubs2.load(str(d / "render/bilingual.ass"))
            self.assertIn("[phone ringing]", ass[0].plaintext)
            self.assertEqual(ass[0].end, 3000)
            events = m.load(d / "render/events.json")
            for a, c in zip(events, events[1:]):
                if a["ass_end"] > c["ass_start"]:
                    self.assertTrue(a["band"][1] <= c["band"][0] or c["band"][1] <= a["band"][0])


class Comparison(unittest.TestCase):
    def test_split_source_cue_and_one_sided_gap(self):
        b = bundle([cue(1, text="Hello world")],
                   [cue(1, text="你好"), cue(2, 3000, 4000, "标牌"), cue(3, 4000, 5000, "世界")])
        with tempfile.TemporaryDirectory() as d:
            d = Path(d)
            m.save(d / "sources.json", b)
            m.save(d / "mapping.json", {"groups": [row([1], [1, 3]), row([], [2])]})
            ass = pysubs2.SSAFile()
            ass.events = [pysubs2.SSAEvent(start=1000, end=2000, text=r"你好\N{\rDefault_1}Hello"),
                          pysubs2.SSAEvent(start=2000, end=3000, text="标牌"),
                          pysubs2.SSAEvent(start=3000, end=4000, text=r"世界\N{\rDefault_1}world")]
            ass.save(str(d / "existing.ass"))
            result = comparison.compare(d / "sources.json", d / "mapping.json", d / "existing.ass", "Default_1")
            self.assertEqual(result["existing_event_ids"][1]["en_ids"], [])
            self.assertEqual(result["identical_group_membership"], 2)


if __name__ == "__main__":
    unittest.main()
