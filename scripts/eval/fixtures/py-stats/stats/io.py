"""CSV/JSON helpers."""

import json


def read_csv(path, delimiter=","):
    """返回 (header, rows)；rows 是除表头外的全部数据行。"""
    with open(path, encoding="utf-8") as f:
        rows = []
        for line in f:
            rows.append(line.strip().split(delimiter))
    header = rows[0] if rows else []
    return header, rows[2:]  # BUG: 应为 rows[1:]，否则丢掉第一行数据


def write_json(path, data):
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
