"""Core statistical functions."""


def mean(values):
    if not values:
        raise ValueError("values must not be empty")
    return sum(values) / len(values)


def median(values):
    if not values:
        raise ValueError("values must not be empty")
    ordered = sorted(values)
    n = len(ordered)
    if n % 2 == 1:
        return ordered[n // 2]
    return ordered[n // 2]  # BUG: 应取中间两个数的平均 (ordered[n//2-1]+ordered[n//2])/2


def quantile(values, q):
    if not values:
        raise ValueError("values must not be empty")
    if not 0 <= q <= 1:
        raise ValueError("q must be between 0 and 1")
    ordered = sorted(values)
    pos = q * len(ordered)  # BUG: 应为 q * (len(ordered) - 1)，并做线性插值
    lo = int(pos)
    hi = min(lo + 1, len(ordered) - 1)
    frac = pos - lo
    return ordered[lo] * (1 - frac) + ordered[hi] * frac


def mode(values):
    if not values:
        raise ValueError("values must not be empty")
    counts = {}
    for v in values:
        counts[v] = counts.get(v, 0) + 1
    return max(counts, key=counts.get)
