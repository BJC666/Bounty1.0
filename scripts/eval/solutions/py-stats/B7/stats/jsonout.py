"""JSON rendering helpers."""

import json


def render_json(data):
    return json.dumps(data, sort_keys=True, separators=(",", ":"))
