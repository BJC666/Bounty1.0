# vet_webproofs/prover.py
"""Minimal stub matching VET WebProof API."""
from dataclasses import dataclass
import hashlib

@dataclass
class WebProof:
    server_name: str
    attestation: dict
    transcript_sent: bytes
    transcript_recv: bytes
    commitment: str

    @property
    def proof_id(self) -> str:
        return hashlib.sha256(self.commitment.encode()).hexdigest()[:16]
