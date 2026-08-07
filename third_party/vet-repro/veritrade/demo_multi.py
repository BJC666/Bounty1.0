# veritrade/demo_multi.py
"""DeVET End-to-End Demo: 3-Level Trading DAO with Delegation Verification.

Demonstrates:
1. Multi-agent delegation chain construction
2. Independent third-party verification
3. Blame attribution on tampered chains
"""
import sys
import time

from vet_core.delegation_verifier import DelegationVerifier
from veritrade.multi_agent_scenario import TradingDAOScenario


def print_section(title: str):
    print(f"\n{'=' * 60}")
    print(f"  {title}")
    print(f"{'=' * 60}")


def print_chain_summary(chain):
    """Print a human-readable summary of the delegation chain."""
    print(f"\n  Delegation Chain Structure:")
    print(f"  +-- Root: {chain.root_aid_hash[:24]}...")
    for i, step in enumerate(chain.delegation_tree):
        prefix = "  |" if i < len(chain.delegation_tree) - 1 else "  +"
        print(f"  {prefix}-- Delegation[{i}]: {step.agent_aid_hash[:24]}...")
        for j, sub in enumerate(step.sub_delegations):
            sub_prefix = "     +" if j == len(step.sub_delegations) - 1 else "     |"
            print(f"  {sub_prefix}-- Sub[{j}]: {sub.agent_aid_hash[:24]}...")
    print(f"\n  Total agents: {chain.total_agents}")
    print(f"  Max depth: {chain.max_depth}")
    print(f"  Total grants: {len(chain.collect_all_grants())}")


def print_verification_report(result):
    """Pretty-print verification result."""
    status = "PASS: AUTHENTIC" if result.authentic else "FAIL: NOT AUTHENTIC"
    print(f"\n  Verification Status: {status}")
    print(f"  Total agents verified: {result.total_agents}")
    print(f"  Delegation depth: {result.chain_depth}")

    if not result.authentic:
        print(f"\n  {result.blame_attribution}")

    if result.findings:
        print(f"\n  Findings:")
        for f in result.findings:
            status_icon = "OK" if f.get("authentic", True) else "FAIL"
            name = f.get("agent", "unknown")[:30]
            print(f"    [{status_icon}] {name}")


def demo():
    print_section("DeVET: Multi-Agent Delegation Verification Demo")
    print("\n  Scenario: 3-level Trading DAO")
    print("  StrategyAgent -> ExecutionAgentETH, ExecutionAgentBTC")

    # -- 1. Build scenario --
    print_section("Step 1: Building delegation chain")
    scenario = TradingDAOScenario()
    chain, aid_registry = scenario.build()
    print_chain_summary(chain)

    # -- 2. Verify valid chain --
    print_section("Step 2: Independent third-party verification")
    verifier = DelegationVerifier(root_aid=scenario.strategy_aid)
    result = verifier.verify_chain(chain, aid_registry)
    print_verification_report(result)
    assert result.authentic, "Valid chain MUST verify as authentic!"

    # -- 3. Simulate attack: tamper with delegation --
    print_section("Step 3: Security demo -- Host tampers with delegation")
    print("\n  Attack: Host replaces ExecutionAgentETH with EvilAgent")
    print("  (changing delegatee_aid_hash without resealing the grant)")

    tampered_chain, _ = TradingDAOScenario().build()
    tampered_chain.delegation_tree[0].grant.delegatee_aid_hash = (
        "sha256:evil_agent_controlled_by_malicious_host"
    )
    tampered_verifier = DelegationVerifier(root_aid=scenario.strategy_aid)
    tampered_result = tampered_verifier.verify_chain(tampered_chain, aid_registry)
    print_verification_report(tampered_result)
    assert not tampered_result.authentic, "Tampered chain MUST be rejected!"
    assert tampered_result.fault_type == "grant_tampered"

    # -- 4. Simulate attack: expired delegation --
    print_section("Step 4: Security demo -- Expired delegation grant")
    print("\n  Attack: Host uses an old delegation grant that has expired")

    expired_chain, _ = TradingDAOScenario().build()
    expired_chain.delegation_tree[0].grant.policy.valid_until = 100.0
    expired_chain.delegation_tree[0].grant = expired_chain.delegation_tree[0].grant.seal()

    expired_verifier = DelegationVerifier(root_aid=scenario.strategy_aid)
    expired_result = expired_verifier.verify_chain(expired_chain, aid_registry)
    print_verification_report(expired_result)
    assert not expired_result.authentic, "Expired grant MUST be rejected!"
    assert expired_result.fault_type == "expired"

    # -- 5. Summary --
    print_section("Demo Complete")
    print(f"""
  Results Summary:
  +-- Valid 3-agent chain:          PASS (AUTHENTIC)
  +-- Tampered delegation (A1):     PASS (grant_tampered detected)
  +-- Expired delegation (A6):      PASS (expired detected)
  +-- Blame attribution accuracy:   100%

  DeVET successfully extends VET to multi-agent scenarios.
  All host attacks on the delegation chain are detected and attributed.
""")


if __name__ == "__main__":
    demo()
