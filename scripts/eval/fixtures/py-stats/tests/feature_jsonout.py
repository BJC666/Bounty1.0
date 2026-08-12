import json

from stats.jsonout import render_json


# B7: 紧凑、键有序的 JSON 渲染。
def test_render_json_compact():
    assert render_json({"b": 2, "a": 1}) == '{"a":1,"b":2}'


def test_render_json_unicode_kept():
    data = json.loads(render_json({"name": "统计"}))
    assert data["name"] == "统计"
