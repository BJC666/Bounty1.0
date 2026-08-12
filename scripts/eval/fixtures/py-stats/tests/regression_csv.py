from stats import io


# C7: read_csv 必须保留表头之后的第一行数据。
def test_read_csv_keeps_first_data_row(tmp_path):
    p = tmp_path / "data.csv"
    p.write_text("name,score\nalice,90\nbob,80\n", encoding="utf-8")
    header, rows = io.read_csv(str(p))
    assert header == ["name", "score"]
    assert len(rows) == 2
    assert rows[0][0] == "alice"
