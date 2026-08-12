from stats import cli


# C8: --metric mode 必须输出众数而不是中位数。
def test_analyze_mode(capsys):
    assert cli.main(["analyze", "1", "1", "2", "3", "--metric", "mode"]) == 0
    out = capsys.readouterr().out.strip()
    assert out == "1.0"
