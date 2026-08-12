import json

from stats import io


def test_write_json(tmp_path):
    target = tmp_path / "out.json"
    io.write_json(str(target), {"a": 1})
    with open(target, encoding="utf-8") as f:
        assert json.load(f) == {"a": 1}
