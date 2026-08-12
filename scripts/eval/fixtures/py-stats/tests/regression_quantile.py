from stats import core


# C6: q=0.5 的四分位数必须等于中位数。
def test_quantile_midpoint():
    assert core.quantile([1, 2, 3, 4], 0.5) == 2.5
