#!/usr/bin/env python3
"""Offline comparison of two ID mapping runs with the same immutable sources."""
import argparse
from collections import Counter
import importlib.util
import json
from pathlib import Path
import statistics
import sys

spec = importlib.util.spec_from_file_location("subtitle_mapping", Path(__file__).with_name("merge.py"))
m = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = m
spec.loader.exec_module(m)


def schema_valid(value):
    if type(value) is not dict or set(value) != {"alignments"} or type(value["alignments"]) is not list:
        return False
    return all(type(r) is dict and set(r) == {"en_ids", "zh_ids", "layer"}
               and r["layer"] in ("top", "bottom")
               and all(type(r[l + "_ids"]) is list and all(type(i) is int for i in r[l + "_ids"])
                       for l in ("en", "zh")) for r in value["alignments"])


def inspect_run(directory, bundle):
    summary = m.load(directory / "initial-run.json")
    ws = {w["index"]: w for w in m.windows(bundle, summary["parameters"]["window_ms"], summary["parameters"]["overlap_ms"])}
    reports, attempts = [], []
    for path in sorted((directory / "requests").glob("*.json")):
        record = m.load(path)
        attempts.extend(record["attempts"])
        w = ws[record["index"]]
        value = record["response"]
        structure = schema_valid(value)
        raw_ids = {l: [i for r in value["alignments"] for i in r[l + "_ids"] if type(i) is int]
                   if structure else [] for l in ("en", "zh")}
        expected = {l: {c["id"] for c in w[l]} for l in ("en", "zh")}
        try:
            valid, rejected = m.validate_response(value, w, summary["parameters"]["max_distance_ms"])
        except ValueError as error:
            valid, rejected = [], [{"reason": str(error)}]
        reports.append({"window": record["index"], "json_schema_shape_valid": structure,
                        "raw_missing": {l: sorted(expected[l] - set(raw_ids[l])) for l in ("en", "zh")},
                        "raw_out_of_window": {l: sorted(set(raw_ids[l]) - expected[l]) for l in ("en", "zh")},
                        "raw_duplicate_ids": {l: sorted(i for i, n in Counter(raw_ids[l]).items() if n > 1) for l in ("en", "zh")},
                        "rejected": rejected,
                        "accepted_mapping_counts": dict(Counter(m.mapping_kind(r) for r in valid)),
                        "attempts": [{k: a.get(k) for k in ("seconds", "model", "finish_reason", "error", "usage")} for a in record["attempts"]]})
    times = [a["seconds"] for a in attempts]
    return {"run": summary, "request_seconds_sum": round(sum(times), 3),
            "request_seconds_min": min(times), "request_seconds_median": statistics.median(times),
            "request_seconds_max": max(times),
            "successful_windows": sum(r["response"] is not None for r in [m.load(p) for p in (directory / "requests").glob("*.json")]),
            "schema_shape_valid_windows": sum(r["json_schema_shape_valid"] for r in reports),
            "raw_missing_occurrences": {l: sum(len(r["raw_missing"][l]) for r in reports) for l in ("en", "zh")},
            "raw_duplicate_id_occurrences": {l: sum(len(r["raw_duplicate_ids"][l]) for r in reports) for l in ("en", "zh")},
            "raw_out_of_window_occurrences": {l: sum(len(r["raw_out_of_window"][l]) for r in reports) for l in ("en", "zh")},
            "reasoning_tokens": sum((a.get("usage") or {}).get("completion_tokens_details", {}).get("reasoning_tokens", 0) for a in attempts),
            "windows": reports}


def compare_inputs(baseline, candidate):
    def requests(directory):
        return {r["index"]: {k: v for k, v in r["request"].items() if k != "model"}
                for r in (m.load(p) for p in (directory / "requests").glob("*.json"))}
    a, b = requests(baseline), requests(candidate)
    return {"same_window_indexes": set(a) == set(b),
            "payloads_equal_except_model": a == b,
            "different_windows": [i for i in sorted(set(a) | set(b)) if a.get(i) != b.get(i)]}


def compare(bundle, left, right):
    for mapping in (left, right):
        m.validate_mapping(mapping, bundle)
    tracks = {l: m.track_dict(bundle, l) for l in ("en", "zh")}
    by_id = lambda mapping: {(l, i): r for r in mapping["groups"] for l in ("en", "zh") for i in r[l + "_ids"]}
    a, b = by_id(left), by_id(right)
    sigs = [{m.signature(r) for r in mapping["groups"]} for mapping in (left, right)]
    membership = lambda r: (tuple(r["en_ids"]), tuple(r["zh_ids"]))
    changed = []
    for r in right["groups"]:
        if m.signature(r) in sigs[0]:
            continue
        keys = {(l, i) for l in ("en", "zh") for i in r[l + "_ids"]}
        related = {m.signature(a[k]): a[k] for k in keys}
        changed.append({"start_ms": min(tracks[l][i].start for l, i in keys), "candidate": r,
                        "baseline": list(related.values()),
                        "text": {l: [{"id": i, "text": tracks[l][i].display} for i in r[l + "_ids"]] for l in ("en", "zh")}})
    focus = []
    for label, es, zs in (("01:14–01:18", [10, 11], [10, 11]), ("04:12–04:22", [45, 46], [46, 47]),
                           ("49:40–49:47", [594, 595], [600, 601])):
        keys = [("en", i) for i in es] + [("zh", i) for i in zs]
        groups = [{m.signature(index[k]): index[k] for k in keys} for index in (a, b)]
        focus.append({"scene": label, "baseline_groups": list(groups[0].values()),
                      "candidate_groups": list(groups[1].values()), "identical_decision": set(groups[0]) == set(groups[1])})
    return {"identical_groups_including_layer": len(sigs[0] & sigs[1]),
            "baseline_groups": len(left["groups"]), "candidate_groups": len(right["groups"]),
            "baseline_only_groups": len(sigs[0] - sigs[1]), "candidate_only_groups": len(sigs[1] - sigs[0]),
            "changed_membership_cues": {l: [i for lang, i in a if lang == l and membership(a[l, i]) != membership(b[l, i])] for l in ("en", "zh")},
            "changed_layer_cues": {l: [i for lang, i in a if lang == l and a[l, i]["layer"] != b[l, i]["layer"]] for l in ("en", "zh")},
            "focus": focus, "differences": changed,
            "limitations": "One run per model; group agreement is not semantic accuracy. Inspect raw responses separately from locally restored groups."}


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--sources", type=Path, required=True)
    p.add_argument("--baseline", type=Path, required=True, help="baseline alignment directory")
    p.add_argument("--candidate", type=Path, required=True, help="candidate alignment directory")
    p.add_argument("--out", type=Path, required=True)
    args = p.parse_args()
    sources = m.source_bundle(args.sources)
    result = {"source_fingerprint": sources["fingerprint"],
              "input_comparison": compare_inputs(args.baseline, args.candidate),
              "baseline": inspect_run(args.baseline, sources), "candidate": inspect_run(args.candidate, sources),
              "decisions": compare(sources, m.load(args.baseline / "mapping.json"), m.load(args.candidate / "mapping.json"))}
    m.save(args.out, result)
    print(json.dumps({"baseline": result["baseline"]["run"], "candidate": result["candidate"]["run"],
                      "decisions": {k: v for k, v in result["decisions"].items() if k not in ("differences", "changed_membership_cues", "changed_layer_cues")}}, ensure_ascii=False, indent=2))
