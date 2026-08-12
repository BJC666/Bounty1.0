# py-stats

小型统计工具包 + CLI。

运行测试：

    python -m pytest tests -q

CLI：

    python -m stats.cli analyze 1 2 3 --metric mean
    python -m stats.cli convert data.csv out.json

模块：

- `stats.core` — mean / median / mode / quantile
- `stats.io` — read_csv / write_json
- `stats.cli` — analyze / convert 子命令
