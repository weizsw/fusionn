#!/usr/bin/env python3
"""Recover source-ID groups from an existing bilingual sidecar with identical text.

No fuzzy matching: both tracks' full ordered non-whitespace text must match.
The primary/secondary style delimiter is explicit, not inferred from language.
"""
import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path
import re

import pysubs2


def norm(s):
    return re.sub(r"\s", "", s)


def compare(sources, mapping, sidecar, secondary_style):
    b = json.loads(sources.read_text(encoding="utf-8"))
    m = json.loads(mapping.read_text(encoding="utf-8"))
    events = pysubs2.load(str(sidecar))
    texts = {"en": [], "zh": []}
    for e in events:
        parts = re.split(r"\\N\{\\r" + re.escape(secondary_style) + r"\}", e.text, maxsplit=1)
        texts["zh"].append(norm(pysubs2.SSAEvent(text=parts[0]).plaintext))
        texts["en"].append(norm(pysubs2.SSAEvent(text=parts[1] if len(parts) == 2 else "").plaintext))
    event_ids = [{"en_ids": [], "zh_ids": [], "start": e.start, "end": e.end} for e in events]
    for lang in ("en", "zh"):
        if "".join(texts[lang]) != "".join(norm(c["display"]) for c in b[lang]):
            raise ValueError(f"{lang}: ordered source glyphs differ; cannot reconstruct comparison IDs exactly")
        intervals, offset = [], 0
        for c in b[lang]:
            length = len(norm(c["display"]))
            if not length:
                raise ValueError("Empty source cue cannot be reconstructed by glyph offsets")
            intervals.append((offset, offset + length, c["id"]))
            offset += length
        offset = 0
        for i, text in enumerate(texts[lang]):
            end = offset + len(text)
            event_ids[i][lang + "_ids"] = [cid for a, z, cid in intervals if text and a < end and z > offset]
            offset = end
    # Connected components recover many-to-one relationships even when an
    # existing renderer divides one source cue across several output events.
    parent = {(lang, c["id"]): (lang, c["id"]) for lang in ("en", "zh") for c in b[lang]}

    def find(k):
        while parent[k] != k:
            parent[k] = parent[parent[k]]
            k = parent[k]
        return k

    for event in event_ids:
        keys = [(l, i) for l in ("en", "zh") for i in event[l + "_ids"]]
        for key in keys[1:]:
            parent[find(key)] = find(keys[0])
    components = {}
    for key in parent:
        g = components.setdefault(find(key), {"en_ids": [], "zh_ids": []})
        g[key[0] + "_ids"].append(key[1])
    existing = list(components.values())
    sig = lambda g: (tuple(g["en_ids"]), tuple(g["zh_ids"]))
    current = {sig(g) for g in m["groups"]}
    old = {sig(g) for g in existing}
    kind = lambda g: "zh_only" if not g["en_ids"] else "en_only" if not g["zh_ids"] else "1:1" if len(g["en_ids"]) == len(g["zh_ids"]) == 1 else "N:1" if len(g["zh_ids"]) == 1 else "1:N" if len(g["en_ids"]) == 1 else "M:N"
    changed = []
    tracks = {l: {c["id"]: c for c in b[l]} for l in ("en", "zh")}
    for g in m["groups"]:
        if sig(g) in old:
            continue
        keys = {(l, i) for l in ("en", "zh") for i in g[l + "_ids"]}
        related = [e for e in existing if keys & {(l, i) for l in ("en", "zh") for i in e[l + "_ids"]}]
        changed.append({"start_ms": min(tracks[l][i]["start"] for l, i in keys), "llm": g, "existing": related,
                        "text": {l: [{"id": i, "text": tracks[l][i]["display"]} for i in g[l + "_ids"]] for l in ("en", "zh")}})
    return {"sidecar": str(sidecar), "sha256": hashlib.sha256(sidecar.read_bytes()).hexdigest(),
            "provenance": "Existing fusionn-shaped sidecar; generator/version not independently verified",
            "ordered_source_glyph_equality": {"en": True, "zh": True},
            "existing_events": len(events), "existing_groups": len(existing),
            "existing_mapping_counts": dict(Counter(kind(g) for g in existing)),
            "identical_group_membership": len(current & old), "llm_groups_with_changed_membership": len(current - old),
            "existing_groups_with_changed_membership": len(old - current),
            "existing_top_events": sum(bool(re.search(r"\\an[789]", e.text)) or int(events.styles[e.style].alignment) >= 7 for e in events),
            "differences": changed, "existing_event_ids": event_ids,
            "limitations": "Text identity verifies same cue content, not model/version provenance. Agreement is not semantic accuracy; font/timing differences remain."}


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--sources", type=Path, required=True)
    p.add_argument("--mapping", type=Path, required=True)
    p.add_argument("--sidecar", type=Path, required=True)
    p.add_argument("--secondary-style", default="Default_1")
    p.add_argument("--out", type=Path, required=True)
    a = p.parse_args()
    result = compare(a.sources, a.mapping, a.sidecar, a.secondary_style)
    a.out.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({k: v for k, v in result.items() if k not in ("differences", "existing_event_ids")}, ensure_ascii=False, indent=2))
