# benchmarks/bench_delegation.py
"""DeVET performance benchmarks: verification latency vs delegation depth."""
import time
import statistics
from veritrade.multi_agent_scenario import TradingDAOScenario
from vet_core.delegation_verifier import DelegationVerifier


def bench_verification_latency(n_runs: int = 50):
    """Benchmark: verification latency for 3-agent chain."""
    scenario = TradingDAOScenario()
    chain, aid_registry = scenario.build()
    verifier = DelegationVerifier(root_aid=scenario.strategy_aid)

    latencies = []
    for _ in range(n_runs):
        t0 = time.perf_counter()
        result = verifier.verify_chain(chain, aid_registry)
        elapsed = time.perf_counter() - t0
        latencies.append(elapsed)
        assert result.authentic

    print("=== Verification Latency (3-agent chain) ===")
    print(f"  Runs:     {n_runs}")
    print(f"  Mean:     {statistics.mean(latencies)*1000:.3f} ms")
    print(f"  Median:   {statistics.median(latencies)*1000:.3f} ms")
    print(f"  P95:      {sorted(latencies)[int(n_runs*0.95)]*1000:.3f} ms")
    print(f"  Min/Max:  {min(latencies)*1000:.3f} / {max(latencies)*1000:.3f} ms")


def bench_blame_attribution_latency(n_runs: int = 50):
    """Benchmark: how fast is blame attribution?"""
    scenario = TradingDAOScenario()
    chain, aid_registry = scenario.build()
    chain.delegation_tree[0].grant.delegatee_aid_hash = "sha256:evil"
    verifier = DelegationVerifier(root_aid=scenario.strategy_aid)

    latencies = []
    for _ in range(n_runs):
        t0 = time.perf_counter()
        result = verifier.verify_chain(chain, aid_registry)
        elapsed = time.perf_counter() - t0
        latencies.append(elapsed)
        assert not result.authentic
        assert result.fault_type is not None

    print("\n=== Blame Attribution Latency (3-agent, fault at depth 0) ===")
    print(f"  Runs:     {n_runs}")
    print(f"  Mean:     {statistics.mean(latencies)*1000:.3f} ms")
    print(f"  Median:   {statistics.median(latencies)*1000:.3f} ms")


def bench_chain_size_overhead():
    """Benchmark: storage overhead of delegation metadata."""
    import json
    scenario = TradingDAOScenario()
    chain, _ = scenario.build()
    grants_json = json.dumps([g.compute_hash() for g in chain.collect_all_grants()])
    print("\n=== Delegation Metadata Overhead ===")
    print(f"  Total grants: {len(chain.collect_all_grants())}")
    print(f"  Grant hashes (serialized): {len(grants_json)} bytes")
    print(f"  Per-agent overhead: ~{len(grants_json) / chain.total_agents:.0f} bytes")


if __name__ == "__main__":
    bench_verification_latency()
    bench_blame_attribution_latency()
    bench_chain_size_overhead()
    print("\n=== All benchmarks complete ===")
