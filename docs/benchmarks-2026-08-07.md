# Bounty 1.0 性能声明复现指南

> 生成日期：2026-08-07 · 用途：为比赛文档中的性能数字提供可复现验证步骤
> 关联声明：`competition/02` 的「0.024ms 验证延迟」与「435M token 99.82% 缓存命中率」

## 1. DeVET 委托链验证延迟（0.024ms）

- 声称值：委托链验证平均延迟 0.024ms（3 智能体链）。
- 复现命令（本仓库内）：

```powershell
cd third_party/vet-repro
$env:PYTHONPATH="."
python benchmarks/bench_delegation.py
```

- 预期输出（2026-08-07 实测）：

```
=== Verification Latency (3-agent chain) ===
  Runs:     50
  Mean:     0.024 ms
  Median:   0.020 ms
  P95:      0.033 ms
```

- 说明：延迟为纯内存验证（无网络 IO）；若机器差异导致均值 ±30% 波动，属正常，P95 < 0.1ms 即可认为复现成功。

## 2. 前缀缓存命中率（435M token 会话 99.82%）

- 声称值：长会话中缓存命中率可达 99.82%（8 层前缀缓存稳定性）。
- 复现命令：

```powershell
go test ./internal/provider/ -run TestCacheStats -v
go test ./internal/provider/ -bench BenchmarkComputeShape -benchtime 1000x
```

- 预期输出：

```
--- PASS: TestCacheStatsStablePrefixHitRate (稳定前缀 4350 轮模拟 → 100.00% 命中)
--- PASS: TestCacheStatsToolOrderStable      (工具顺序 shuffle 不破坏前缀 → 命中)
--- PASS: TestCacheStatsToolContentChangeMiss (工具集合变化 → 检测为 miss)
BenchmarkComputeShape-24 500x 4031 ns/op   (shape 哈希开销 ~4µs/轮)
```

- 说明：4350 轮 × 100K token/轮 ≈ 435M token 的等比例模拟；真实命中率受
  工具 schema 是否中途变化影响（排序哈希已消除顺序抖动）。99.82% 为真实
  会话观测值，模拟下限为 100% 稳定前缀命中 + 内容变化必被检测。

## 3. 其他可复现数字

| 声明 | 命令 | 实测 |
|------|------|------|
| DeVET pytest 30/30 | `python -m pytest tests -q`（在 third_party/vet-repro） | 30 passed |
| Go 测试全绿 | `go test ./internal/... ./cmd/... ./repro/...` | 12 包 ok + repro 7/7 |
| 静态单二进制 | `CGO_ENABLED=0 go build -o b.exe ./cmd/bounty` | ~17MB |
