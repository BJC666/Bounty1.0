import pytest

from stats.variance import variance, stddev


# B5: 方差与标准差（总体方差）。
def test_variance():
    assert variance([2, 4, 4, 4, 5, 5, 7, 9]) == pytest.approx(4.0)


def test_stddev():
    assert stddev([2, 4, 4, 4, 5, 5, 7, 9]) == pytest.approx(2.0)
