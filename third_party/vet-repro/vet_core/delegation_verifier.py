# vet_core/delegation_verifier.py
"""DeVET: Recursive delegation verifier with blame attribution."""
from dataclasses import dataclass, field
from typing import Dict, List, Optional
import time

from vet_core.aid import AID
from vet_core.compositional import CompositionalVerifier
from vet_core.delegation import (
    DelegationExecutionStep,
    DelegationExecutionChain,
)


@dataclass
class DelegationVerificationResult:
    """Complete result of delegation chain verification.

    On failure, blame_path pinpoints the first fault and fault_type
    tells what went wrong -- enabling precise attribution.
    """
    authentic: bool
    blame_path: List[str] = field(default_factory=list)
    error: Optional[str] = None
    fault_type: Optional[str] = None
    findings: List[dict] = field(default_factory=list)
    trace: List[dict] = field(default_factory=list)
    chain_depth: int = 0
    total_agents: int = 0

    @property
    def blame_attribution(self) -> str:
        """Human-readable blame report."""
        if self.authentic:
            return "未检测到故障——委托链真实有效。"

        fault_labels = {
            "grant_tampered": "授权层：委托授权被篡改（授权哈希不匹配）",
            "policy_violation": "策略层：委托违反声明的约束条件",
            "subagent_proof_invalid": "证明层：子智能体执行证明无效",
            "expired": "时间层：委托授权已过期",
            "chain_broken": "链路层：父授权引用断裂",
            "root_aid_mismatch": "根层：委托链声明了不同的根智能体",
        }
        label = fault_labels.get(self.fault_type, self.fault_type)

        return (
            f"故障位置：{' -> '.join(self.blame_path)}\n"
            f"类型：{label}\n"
            f"详情：{self.error}"
        )


class DelegationVerifier:
    """Recursive verifier for multi-agent delegation chains.

    Traverses the delegation tree depth-first. At each node:
    1. Verifies grant integrity (hash matches, not expired)
    2. Checks policy compliance (depth, endpoints, etc.)
    3. Verifies the agent's VET composite proof
    4. Recurses into sub-delegations

    The FIRST verification failure is returned -- this is the blame point.
    """

    def __init__(self, root_aid: AID):
        """Initialize with the root AID that the user trusts."""
        self.root_aid = root_aid

    def verify_chain(
        self,
        chain: DelegationExecutionChain,
        all_aids: Dict[str, AID],
    ) -> DelegationVerificationResult:
        """Verify a complete delegation execution chain.

        Args:
            chain: The delegation chain to verify
            all_aids: Mapping of AID hash -> AID for all agents in chain

        Returns:
            DelegationVerificationResult with authentic=True only if
            the entire chain passes all checks.
        """
        findings = []
        trace = []
        t_start = time.perf_counter()

        def _chk(check, ok, detail):
            trace.append({
                "check": check,
                "ok": ok,
                "detail": detail,
                "t_ms": round((time.perf_counter() - t_start) * 1000, 3),
            })

        # Step 1: Root AID identity
        ok1 = chain.root_aid_hash == self.root_aid.agent_hash
        _chk("根身份校验", ok1,
             f"链根哈希 {chain.root_aid_hash[:16]}... vs 信任根 {self.root_aid.agent_hash[:16]}...")
        if not ok1:
            return DelegationVerificationResult(
                authentic=False,
                blame_path=["root"],
                error=(
                    f"根身份哈希不匹配：链声明 "
                    f"{chain.root_aid_hash}，验证方持有 {self.root_aid.agent_hash}"
                ),
                fault_type="root_aid_mismatch",
                trace=trace,
            )

        # Step 2: Root chain continuity
        root_ok, root_msg = chain.root_chain.verify_continuity()
        _chk("根执行链连续性", root_ok, root_msg)
        if not root_ok:
            return DelegationVerificationResult(
                authentic=False,
                blame_path=["root"],
                error=f"根执行链断裂：{root_msg}",
                fault_type="chain_broken",
                trace=trace,
            )

        findings.append({"agent": "root", "chain_ok": True})

        # Step 3: Recursively verify delegation tree
        for i, step in enumerate(chain.delegation_tree):
            result = self._verify_step(
                step, all_aids, path=[f"delegation[{i}]"], trace=trace, t_start=t_start
            )
            findings.append({
                "agent": step.agent_aid_hash[:20],
                "authentic": result.authentic,
                "fault_type": result.fault_type,
            })
            if not result.authentic:
                return result  # Return first failure (blame point)

        return DelegationVerificationResult(
            authentic=True,
            blame_path=[],
            error=None,
            findings=findings,
            chain_depth=chain.max_depth,
            total_agents=chain.total_agents,
            trace=trace,
        )

    def _verify_step(
        self,
        step: DelegationExecutionStep,
        all_aids: Dict[str, AID],
        path: List[str],
        trace: List[dict],
        t_start: float,
    ) -> DelegationVerificationResult:
        """Recursively verify a single delegation step and its sub-delegations.

        Order of checks matters for blame attribution:
        grant integrity -> policy -> proof -> sub-delegations
        """
        grant = step.grant
        node = " → ".join(path)

        def _chk(check, ok, detail):
            trace.append({
                "check": check,
                "ok": ok,
                "detail": detail,
                "t_ms": round((time.perf_counter() - t_start) * 1000, 3),
            })

        # Check 1: Grant integrity (not tampered)
        ok, msg = grant.verify_integrity()
        expected = grant.compute_hash()
        _chk(f"{node} · 授权完整性（哈希封存）", ok,
             (f"重算哈希 {expected[:16]}... vs 存储哈希 {grant.grant_hash[:16]}...") if not ok
             else f"重算哈希与存储哈希一致（{expected[:16]}...）")
        if not ok:
            return DelegationVerificationResult(
                authentic=False,
                blame_path=path,
                error=msg,
                fault_type="grant_tampered",
                trace=trace,
            )

        # Check 2: Grant not expired
        expired = grant.policy.is_expired()
        _chk(f"{node} · 有效期检查", not expired,
             f"过期时间 {grant.policy.valid_until:.1f}，当前 {time.time():.1f}"
             + ("（已过期）" if expired else "（有效）"))
        if expired:
            return DelegationVerificationResult(
                authentic=False,
                blame_path=path,
                error=(
                    f"委托授权已于 {grant.policy.valid_until} 过期，"
                    f"当前时间 {time.time()}"
                ),
                fault_type="expired",
                trace=trace,
            )

        # Check 3: Policy -- sub-delegation depth limit
        depth_ok = not (step.sub_delegations and grant.policy.max_delegation_depth == 0)
        _chk(f"{node} · 委托深度策略", depth_ok,
             f"子委托 {len(step.sub_delegations)} 个 / 允许深度 {grant.policy.max_delegation_depth}")
        if not depth_ok:
            return DelegationVerificationResult(
                authentic=False,
                blame_path=path,
                error=(
                    f"检测到子委托，但策略 max_delegation_depth=0（"
                    f"智能体 {grant.delegatee_aid_hash[:20]}...）"
                ),
                fault_type="policy_violation",
                trace=trace,
            )

        # Check 4: Sub-agent proof valid
        delegatee_aid = all_aids.get(grant.delegatee_aid_hash)
        _chk(f"{node} · 受托方 AID 注册表", delegatee_aid is not None,
             f"受托方 {grant.delegatee_aid_hash[:16]}... "
             + ("已注册" if delegatee_aid is not None else "缺失"))
        if delegatee_aid is None:
            return DelegationVerificationResult(
                authentic=False,
                blame_path=path,
                error=(
                    f"受托方 AID {grant.delegatee_aid_hash[:20]}... "
                    f"未在提供的 AID 注册表中找到"
                ),
                fault_type="subagent_proof_invalid",
                trace=trace,
            )

        # Check 4b: required_aid_hash constraint
        if grant.policy.required_aid_hash:
            req_ok = grant.policy.required_aid_hash == grant.delegatee_aid_hash
            _chk(f"{node} · required_aid_hash 约束", req_ok,
                 f"策略要求 {grant.policy.required_aid_hash[:16]}... vs 受托方 {grant.delegatee_aid_hash[:16]}...")
            if not req_ok:
                return DelegationVerificationResult(
                    authentic=False,
                    blame_path=path,
                    error=(
                        f"AID 约束被违反：策略要求 "
                        f"{grant.policy.required_aid_hash}, "
                        f"但实际受托方是 {grant.delegatee_aid_hash}"
                    ),
                    fault_type="policy_violation",
                    trace=trace,
                )

        sub_verifier = CompositionalVerifier(aid=delegatee_aid)
        sub_ok, sub_msg = sub_verifier.verify(step.agent_proof)
        _chk(f"{node} · 子智能体执行证明", sub_ok, sub_msg)
        if not sub_ok:
            return DelegationVerificationResult(
                authentic=False,
                blame_path=path + [delegatee_aid.agent_name],
                error=f"子智能体证明无效：{sub_msg}",
                fault_type="subagent_proof_invalid",
                trace=trace,
            )

        # Check 5: API call limit
        actual_calls = len(step.agent_proof.all_sub_proofs())
        limit_ok = actual_calls <= grant.policy.max_api_calls
        _chk(f"{node} · API 调用上限", limit_ok,
             f"实际 {actual_calls} 次 vs 上限 {grant.policy.max_api_calls} 次")
        if not limit_ok:
            return DelegationVerificationResult(
                authentic=False,
                blame_path=path,
                error=(
                    f"API 调用次数超限：实际 {actual_calls} 次，"
                    f"策略允许 {grant.policy.max_api_calls} 次"
                ),
                fault_type="policy_violation",
                trace=trace,
            )

        # Check 6: Recursively verify sub-delegations
        for j, sub_step in enumerate(step.sub_delegations):
            sub_result = self._verify_step(
                sub_step, all_aids, path=path + [f"sub[{j}]"], trace=trace, t_start=t_start
            )
            if not sub_result.authentic:
                return sub_result

        return DelegationVerificationResult(
            authentic=True,
            blame_path=[],
            error=None,
            trace=trace,
        )
