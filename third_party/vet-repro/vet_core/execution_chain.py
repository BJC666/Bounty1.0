# vet_core/execution_chain.py
"""Minimal stub matching VET ExecutionChain API."""
from dataclasses import dataclass, field
from typing import List, Optional
import hashlib
import json


@dataclass
class ChainedStep:
    step_index: int
    timestamp: float
    prev_hash: Optional[str]
    notary_commitments: List[str]
    step_hash: str = ""

    def compute_hash(self) -> str:
        payload = json.dumps({
            "index": self.step_index,
            "timestamp": self.timestamp,
            "prev_hash": self.prev_hash,
            "commitments": sorted(self.notary_commitments),
        }, sort_keys=True)
        return "sha256:" + hashlib.sha256(payload.encode()).hexdigest()

    def seal(self) -> "ChainedStep":
        self.step_hash = self.compute_hash()
        return self


@dataclass
class ExecutionChain:
    chain_id: str
    steps: List[ChainedStep] = field(default_factory=list)

    def append(self, step: ChainedStep) -> "ExecutionChain":
        if self.steps:
            step.prev_hash = self.steps[-1].step_hash
            step.step_index = len(self.steps)
        else:
            step.prev_hash = None
            step.step_index = 0
        step.seal()
        self.steps.append(step)
        return self

    def verify_continuity(self) -> tuple:
        if not self.steps:
            return True, "Empty chain"
        for i, step in enumerate(self.steps):
            if step.step_index != i:
                return False, f"执行链断点：第 {i} 步，期望序号 {i}，实际 {step.step_index}"
            if i == 0:
                if step.prev_hash is not None:
                    return False, "第一步的 prev_hash 必须为空"
            else:
                expected = self.steps[i - 1].step_hash
                if step.prev_hash != expected:
                    return False, f"执行链在第 {i} 步断裂"
            if step.step_hash != step.compute_hash():
                return False, f"第 {i} 步：哈希不匹配"
        return True, f"执行链验证通过：共 {len(self.steps)} 步"
