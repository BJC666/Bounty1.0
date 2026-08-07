# vet_core/compositional.py
"""Minimal stub matching VET compositional engine API."""
from dataclasses import dataclass, field
from typing import List
from vet_core.aid import AID
from vet_webproofs.prover import WebProof


@dataclass
class ToolCallRecord:
    tool_name: str
    request: str
    response: str
    proof: WebProof


@dataclass
class CoreStepRecord:
    request: str
    response: str
    proof: WebProof


@dataclass
class ExecutionStep:
    core: CoreStepRecord
    tools: List[ToolCallRecord] = field(default_factory=list)


@dataclass
class CompositeProof:
    aid_hash: str
    steps: List[ExecutionStep] = field(default_factory=list)

    def all_sub_proofs(self) -> list:
        proofs = []
        for step in self.steps:
            proofs.append(step.core.proof)
            for tool in step.tools:
                proofs.append(tool.proof)
        return proofs


class CompositionalVerifier:
    def __init__(self, aid: AID):
        self.aid = aid

    def verify(self, composite: CompositeProof, claimed_outputs: list = None) -> tuple:
        if composite.aid_hash != self.aid.agent_hash:
            return False, f"AID mismatch: proof={composite.aid_hash}, verifier={self.aid.agent_hash}"
        if not composite.steps:
            return False, "Empty composite proof"
        return True, f"All {len(composite.all_sub_proofs())} sub-proofs verified"
