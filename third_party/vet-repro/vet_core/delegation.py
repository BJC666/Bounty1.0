# vet_core/delegation.py
"""DeVET: Cross-Agent Delegation Verification Chain.

Extends VET's single-agent verification to multi-agent delegation scenarios.
"""
from dataclasses import dataclass, field
from typing import List, Optional, TYPE_CHECKING
import hashlib
import json
import time

if TYPE_CHECKING:
    from vet_core.compositional import CompositeProof
    from vet_core.execution_chain import ExecutionChain


@dataclass
class DelegationPolicy:
    """Constraints that delegator Agent A imposes on delegatee Agent B.

    Every field is covered by the grant_hash, so any tampering is detected.
    """
    allowed_endpoints: List[str]       # Domains B is permitted to contact
    allowed_models: List[str]          # LLM models B may use
    valid_until: float                 # Unix timestamp, delegation expiry
    max_delegation_depth: int = 0      # May B sub-delegate? 0 = no
    max_api_calls: int = 10            # Max API calls B may make
    required_aid_hash: Optional[str] = None  # If set, B MUST have this AID hash

    def __post_init__(self):
        if not self.allowed_endpoints:
            raise ValueError("allowed_endpoints cannot be empty")
        if not self.allowed_models:
            raise ValueError("allowed_models cannot be empty")
        if self.valid_until <= 0:
            raise ValueError("valid_until must be positive")
        if self.max_delegation_depth < 0:
            raise ValueError("max_delegation_depth cannot be negative")
        if self.max_api_calls < 1:
            raise ValueError("max_api_calls must be at least 1")
        if self.required_aid_hash is not None and not self.required_aid_hash.startswith("sha256:"):
            raise ValueError("required_aid_hash must start with 'sha256:'")

    def is_expired(self) -> bool:
        """Check if this delegation has expired."""
        return time.time() > self.valid_until

    def allows_endpoint(self, endpoint: str) -> bool:
        """Check if an endpoint is within allowed list."""
        return endpoint in self.allowed_endpoints

    def allows_model(self, model: str) -> bool:
        """Check if a model is within allowed list."""
        return model in self.allowed_models

    def to_dict(self) -> dict:
        return {
            "allowed_endpoints": sorted(self.allowed_endpoints),
            "allowed_models": sorted(self.allowed_models),
            "valid_until": self.valid_until,
            "max_delegation_depth": self.max_delegation_depth,
            "max_api_calls": self.max_api_calls,
            "required_aid_hash": self.required_aid_hash,
        }


@dataclass
class DelegationGrant:
    """Cryptographic delegation authorization: Agent A -> Agent B.

    The grant_hash covers all fields, making any tampering detectable.
    The notary_attestation_ref ties this grant to the delegator's execution
    proof, proving the delegation decision was authentically made by A.
    """
    delegator_aid_hash: str           # A's AID hash -- who is delegating
    delegatee_aid_hash: str           # B's AID hash -- who receives delegation
    policy: DelegationPolicy          # What B is allowed to do
    issuance_timestamp: float         # When this grant was issued
    notary_attestation_ref: str       # Notary commitment from A's execution step
    parent_grant_hash: Optional[str] = None  # If A was itself delegated, parent grant
    grant_hash: str = ""              # SHA-256 of all above fields

    def compute_hash(self) -> str:
        payload = json.dumps({
            "delegator_aid_hash": self.delegator_aid_hash,
            "delegatee_aid_hash": self.delegatee_aid_hash,
            "policy": self.policy.to_dict(),
            "parent_grant_hash": self.parent_grant_hash,
            "issuance_timestamp": self.issuance_timestamp,
            "notary_attestation_ref": self.notary_attestation_ref,
        }, sort_keys=True)
        return "sha256:" + hashlib.sha256(payload.encode()).hexdigest()

    def seal(self) -> "DelegationGrant":
        """Lock the grant by computing its hash. Irreversible."""
        self.grant_hash = self.compute_hash()
        return self

    def verify_integrity(self) -> tuple:
        """Check that grant_hash matches computed hash (not tampered)."""
        expected = self.compute_hash()
        if self.grant_hash != expected:
            return False, (
                f"授权哈希不匹配：存储值={self.grant_hash[:20]}...，"
                f"重算值={expected[:20]}..."
            )
        return True, "授权完整性验证通过"


@dataclass
class DelegationExecutionStep:
    """One delegated agent's execution within the chain.

    Contains the delegation grant that authorized this agent,
    the agent's own composite execution proof, and any sub-delegations
    this agent made to other agents.
    """
    grant: DelegationGrant                       # Who delegated to this agent
    agent_proof: "CompositeProof"               # This agent's execution proof (VET)
    sub_delegations: List["DelegationExecutionStep"] = field(default_factory=list)

    def __post_init__(self):
        if self.grant.grant_hash == "":
            raise ValueError("Grant must be sealed before creating DelegationExecutionStep")

    @property
    def agent_aid_hash(self) -> str:
        """The AID hash of the agent that executed this step."""
        return self.grant.delegatee_aid_hash

    @property
    def total_sub_proofs(self) -> int:
        """Total number of sub-proofs including all nested delegations."""
        count = len(self.agent_proof.all_sub_proofs())
        for sub in self.sub_delegations:
            count += sub.total_sub_proofs
        return count


@dataclass
class DelegationExecutionChain:
    """Complete multi-agent delegation execution chain.

    Root agent is the user's entry point -- the only agent the user
    explicitly trusts. All other agents in the chain must prove they
    were legitimately delegated.
    """
    root_aid_hash: str                           # User-trusted root agent
    root_chain: "ExecutionChain"                 # Root agent's execution chain
    delegation_tree: List[DelegationExecutionStep] = field(default_factory=list)

    def flatten(self) -> List[DelegationExecutionStep]:
        """DFS traversal of the delegation tree."""
        result = []
        for step in self.delegation_tree:
            result.append(step)
            result.extend(self._flatten_sub(step))
        return result

    def _flatten_sub(self, step: DelegationExecutionStep) -> List[DelegationExecutionStep]:
        result = []
        for sub in step.sub_delegations:
            result.append(sub)
            result.extend(self._flatten_sub(sub))
        return result

    @property
    def total_agents(self) -> int:
        """Total number of agents in the chain (root + all delegates)."""
        return 1 + len(self.flatten())

    @property
    def max_depth(self) -> int:
        """Maximum delegation depth (0 = no delegation)."""
        def depth(step: DelegationExecutionStep) -> int:
            if not step.sub_delegations:
                return 1
            return 1 + max(depth(s) for s in step.sub_delegations)
        if not self.delegation_tree:
            return 0
        return max(depth(s) for s in self.delegation_tree)

    def collect_all_grants(self) -> List[DelegationGrant]:
        """Collect all delegation grants in the tree."""
        grants = []
        for step in self.flatten():
            grants.append(step.grant)
        return grants
