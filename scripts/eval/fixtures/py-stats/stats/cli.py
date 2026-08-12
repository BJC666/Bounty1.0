"""Command-line interface."""

import argparse

from stats import core


def main(argv=None):
    parser = argparse.ArgumentParser(prog="stats")
    sub = parser.add_subparsers(dest="command", required=True)

    a = sub.add_parser("analyze")
    a.add_argument("values", nargs="+", type=float)
    a.add_argument("--metric", choices=["mean", "median", "mode"], default="mean")

    c = sub.add_parser("convert")
    c.add_argument("input")
    c.add_argument("output")

    args = parser.parse_args(argv)

    if args.command == "analyze":
        values = list(args.values)
        if args.metric == "mean":
            result = core.mean(values)
        elif args.metric == "median":
            result = core.median(values)
        else:
            result = core.median(values)  # BUG: 应为 core.mode(values)
        print(result)
    elif args.command == "convert":
        from stats import io

        header, rows = io.read_csv(args.input)
        data = [dict(zip(header, row)) for row in rows]
        io.write_json(args.output, data)
        print(f"wrote {args.output}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
