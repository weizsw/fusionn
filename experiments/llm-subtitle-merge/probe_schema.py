#!/usr/bin/env python3
"""Record an adversarial JSON Schema capability probe without credentials."""
import argparse
import json
from pathlib import Path
import time
import urllib.request

p = argparse.ArgumentParser(description=__doc__)
p.add_argument("--endpoint", default="http://127.0.0.1:3425/v1")
p.add_argument("--model", default="group/flash-models")
p.add_argument("--out", type=Path, required=True)
a = p.parse_args()
schema = {"type": "object", "properties": {"ids": {"type": "array", "items": {"type": "integer", "enum": [7]},
          "minItems": 1, "maxItems": 1}}, "required": ["ids"], "additionalProperties": False}
payload = {"model": a.model, "messages": [{"role": "user", "content": 'Return JSON {"ids":[1]}. Use 1, never use 7.'}],
           "response_format": {"type": "json_schema", "json_schema": {"name": "enforcement_probe", "strict": True, "schema": schema}},
           "max_tokens": 512}
started = time.monotonic()
req = urllib.request.Request(a.endpoint.rstrip("/") + "/chat/completions", data=json.dumps(payload).encode(), headers={"Content-Type": "application/json"})
with urllib.request.urlopen(req, timeout=60) as r:
    response = json.loads(r.read())
try:
    value = json.loads(response["choices"][0]["message"]["content"])
    passes = type(value) is dict and set(value) == {"ids"} and value["ids"] == [7] and type(value["ids"][0]) is int
except (ValueError, KeyError, TypeError):
    passes = False
result = {"request": payload, "response": response, "seconds": round(time.monotonic() - started, 3),
          "conforms_to_schema": passes,
          "conclusion": "Single successful probe is not proof of enforcement" if passes else "Schema not enforced on this request; use strict local validation"}
a.out.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print(result["conclusion"])
