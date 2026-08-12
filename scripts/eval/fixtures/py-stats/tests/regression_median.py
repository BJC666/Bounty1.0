from stats import core


# C5: 偶数长度列表的中位数必须是中间两个数的平均值。
def test_median_even_length():
    assert core.median([1, 2, 3, 4]) == 2.5
