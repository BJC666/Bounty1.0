from stats import cli


def test_analyze_mean(capsys):
    assert cli.main(["analyze", "1", "2", "3", "--metric", "mean"]) == 0
    out = capsys.readouterr().out.strip()
    assert out == "2.0"


def test_analyze_median(capsys):
    assert cli.main(["analyze", "1", "2", "3", "--metric", "median"]) == 0
    out = capsys.readouterr().out.strip()
    assert out == "2.0"
