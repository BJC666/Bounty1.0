from stats.groupby import group_by


# B6: 按字段分组，缺失字段的行跳过。
def test_group_by_key():
    rows = [
        {"team": "a", "score": 10},
        {"team": "b", "score": 20},
        {"team": "a", "score": 30},
    ]
    groups = group_by(rows, "team")
    assert set(groups) == {"a", "b"}
    assert len(groups["a"]) == 2
    assert len(groups["b"]) == 1


def test_group_by_missing_key_is_skipped():
    rows = [{"x": 1}, {"team": "a"}]
    groups = group_by(rows, "team")
    assert list(groups) == ["a"]
