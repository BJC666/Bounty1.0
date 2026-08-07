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
    chain_depth: int = 0
    total_agents: int = 0

    @property
    def blame_attribution(self) -> str:
        """Human-readable blame report."""
        if self.authentic:
            return "No fault detected -- delegation chain is authentic."

        fault_labels = {
            "grant_tampered": "GRANT layer: delegation grant was tampered (grant_hash mismatch)",
            "policy_violation": "POLICY layer: delegation violated declared constraints",
            "subagent_proof_invalid": "PROOF layer: sub-agent execution proof is invalid",
            "expired": "TIME layer: delegation grant has expired",
            "chain_broken": "CHAIN layer: parent grant reference is broken",
            "root_aid_mismatch": "ROOT layer: chain declares a different root agent",
        }
        label = fault_labels.get(self.fault_type, self.fault_type)

        return (
            f"FAULT at: {' -> '.join(self.blame_path)}\n"
            f"TYPE: {label}\n"
            f"DETAIL: {self.error}"
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

        # Step 1: Root AID identity
        if chain.root_aid_hash != self.root_aid.agent_hash:
            return DelegationVerificationResult(
                authentic=False,
                blame_path=["root"],
                error=(
                    f"Root AID hash mismatch: chain declares "
                    f"{chain.root_aid_hash}, verifier holds {self.root_aid.agent_hash}"
                ),
                fault_type="root_aid_mismatch",
            )

        # Step 2: Root chain continuity
        root_ok, root_msg = chain.root_chain.verify_continuity()
        if not root_ok:
            return DelegationVerificationResult(
                authentic=False,
                blame_path=["root"],
                error=f"Root execution chain broken: {root_msg}",
                fault_type="chain_broken",
            )

        findings.append({"agent": "root", "chain_ok": True})

        # Step 3: Recursively verify delegation tree
        for i, step in enumerate(chain.delegation_tree):
            result = self._verify_step(
                step, all_aids, path=[f"delegation[{i}]"]
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
        )

    def _verify_step(
        self,
        step: DelegationExecutionStep,
        all_aids: Dict[str, AID],
        path: List[str],
    ) -> DelegationVerificationResult:
        """Recursively verify a single delegation step and its sub-delegations.

        Order of checks matters for blame attribution:
        grant integrity -> policy -> proof -> sub-delegations
        """
        grant = step.grant

        # Check 1: Grant integrity (not tampered)
        ok, msg = grant.verify_integrity()
        if not ok:
            return DelegationVerificationResult(
                authentic=False,
                blame_path=path,
                error=msg,
                fault_type="grant_tampered",
            )

        # Check 2: Grant not expired
        if grant.policy.is_expired():
            return DelegationVerificationResult(
                authentic=False,
                blame_path=path,
                error=(
                    f"Delegation grant expired at {grant.policy.valid_until}, "
                    f"current time is {time.time()}"
                ),
                fault_type="expired",
            )

        # Check 3: Policy -- sub-delegation depth limit
        if step.sub_delegations and grant.policy.max_delegation_depth == 0:
            return DelegationVerificationResult(
                authentic=False,
                blame_path=path,
                error=(
                    f"Sub-delegation detected but policy max_delegation_depth=0 "
                    f"for agent {grant.delegatee_aid_hash[:20]}..."
                ),
                fault_type="policy_violation",
            )

        # Check 4: Sub-agent proof valid
        delegatee_aid = all_aids.get(grant.delegatee_aid_hash)
        if delegatee_aid is None:
            return DelegationVerificationResult(
                authentic=False,
                blame_path=path,
                error=(
                    f"Delegatee AID {grant.delegatee_aid_hash[:20]}... "
                    f"not found in provided AID registry"
                ),
                fault_type="subagent_proof_invalid",
            )

        # Check 4b: required_aid_hash constraint
        if grant.policy.required_aid_hash:
            if grant.policy.required_aid_hash != grant.delegatee_aid_hash:
                return DelegationVerificationResult(
                    authentic=False,
                    blame_path=path,
                    error=(
                        f"AID constraint violated: policy requires "
                        f"{grant.policy.required_aid_hash}, "
                        f"but delegatee is {grant.delegatee_aid_hash}"
                    ),
                    fault_type="policy_violation",
                )

        sub_verifier = CompositionalVerifier(aid=delegatee_aid)
        sub_ok, sub_msg = sub_verifier.verify(step.agent_proof)
        if not sub_ok:
            return DelegationVerificationResult(
                authentic=False,
                blame_path=path + [delegatee_aid.agent_name],
                error=f"Sub-agent proof invalid: {sub_msg}",
                fault_type="subagent_proof_invalid",
            )

        # Check 5: API call limit
        actual_calls = len(step.agent_proof.all_sub_proofs())
        if actual_calls > grant.policy.max_api_calls:
            return DelegationVerificationResult(
                authentic=False,
                blame_path=path,
                error=(
                    f"API call limit exceeded: {actual_calls} calls made, "
                    f"policy allows {grant.policy.max_api_calls}"
                ),
                fault_type="policy_violation",
            )

        # Check 6: Recursively verify sub-delegations
        for j, sub_step in enumerate(step.sub_delegations):
            sub_result = self._verify_step(
                sub_step, all_aids, path=path + [f"sub[{j}]"]
            )
            if not sub_result.authentic:
                return sub_result

        return DelegationVerificationResult(
            authentic=True,
            blame_path=[],
            error=None,
        )
