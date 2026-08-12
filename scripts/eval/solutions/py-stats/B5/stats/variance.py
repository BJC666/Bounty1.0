"""Variance and standard deviation (population)."""


def variance(values):
    if not values:
        raise ValueError("values must not be empty")
    m = sum(values) / len(values)
    return sum((v - m) ** 2 for v in values) / len(values)


def stddev(values):
    return variance(values) ** 0.5
