import pytest

from stats import core


def test_mean():
    assert core.mean([1, 2, 3]) == 2


def test_mean_empty_raises():
    with pytest.raises(ValueError):
        core.mean([])


def test_median_odd_length():
    assert core.median([3, 1, 2]) == 2


def test_mode():
    assert core.mode([1, 2, 2, 3]) == 2


def test_quantile_out_of_range_raises():
    with pytest.raises(ValueError):
        core.quantile([1], 1.5)
