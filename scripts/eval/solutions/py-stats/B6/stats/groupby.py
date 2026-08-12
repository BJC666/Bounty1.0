"""Group rows by a key."""


def group_by(rows, key):
    groups = {}
    for row in rows:
        if key not in row:
            continue
        groups.setdefault(row[key], []).append(row)
    return groups
