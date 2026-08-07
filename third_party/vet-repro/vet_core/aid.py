# vet_core/aid.py
"""Minimal stub matching VET AID API."""
from pydantic import BaseModel, Field
from typing import List, Optional, Literal
from enum import Enum
import hashlib
import json


class ProofSystem(str, Enum):
    TLS_NOTARY = "TLSNotary"
    TEE_PROXY = "TEEProxy"
    CONSENSUS = "Consensus"


class VerificationMeta(BaseModel):
    proof_system: ProofSystem = ProofSystem.TLS_NOTARY
    protocol_version: Optional[str] = None
    notary_public_key: Optional[str] = None
    tee_type: Optional[Literal["SGX", "TDX"]] = None
    committee_id: Optional[str] = None
    committee_size: Optional[int] = None


class ComponentSpec(BaseModel):
    name: str
    endpoint: str
    injection_algorithm_uid: str
    parsing_algorithm_uid: str
    verification: VerificationMeta = VerificationMeta()


class AID(BaseModel):
    agent_name: str
    core: ComponentSpec
    tools: List[ComponentSpec] = Field(default_factory=list)
    agent_hash: str = ""

    def compute_hash(self) -> str:
        data = self.model_dump(exclude={"agent_hash"})
        canonical = json.dumps(data, sort_keys=True, ensure_ascii=False)
        return "sha256:" + hashlib.sha256(canonical.encode()).hexdigest()

    def seal(self) -> "AID":
        self.agent_hash = self.compute_hash()
        return self
