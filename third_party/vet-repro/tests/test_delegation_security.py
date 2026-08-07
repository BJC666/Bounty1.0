# tests/test_delegation_security.py
"""DeVET security tests: 8 attack scenarios with blame attribution verification."""
import time
import pytest

from vet_core.aid import AID, ComponentSpec
from vet_core.compositional import CompositeProof, ExecutionStep, CoreStepRecord, ToolCallRecord
from vet_core.execution_chain import ExecutionChain, ChainedStep
from vet_webproofs.prover import WebProof
from vet_core.delegation import (
    DelegationPolicy, DelegationGrant,
    DelegationExecutionStep, DelegationExecutionChain,
)
from vet_core.delegation_verifier import DelegationVerifier


# ============================================================
# Test fixtures
# ============================================================

def make_aid(name="Agent", endpoint="api.openai.com"):
    return AID(
        agent_name=name,
        core=ComponentSpec(
            name="Core", endpoint=endpoint,
            injection_algorithm_uid=f"sha256:inj_{name}",
            parsing_algorithm_uid=f"sha256:par_{name}",
        ),
    ).seal()


def make_webproof(server="api.openai.com", seed="test"):
    import hashlib
    commitment = "sha256:" + hashlib.sha256(seed.encode()).hexdigest()
    return WebProof(
        server_name=server,
        attestation={
            "body": {"server_name": server, "transcript_commitment": commitment},
            "signature": "0x" + hashlib.sha256(f"sig_{seed}".encode()).hexdigest(),
        },
        transcript_sent=b"GET / HTTP/1.1\r\n\r\n",
        transcript_recv=b"HTTP/1.1 200 OK\r\n\r\n{}",
        commitment=commitment,
    )


def make_composite(aid):
    wp = make_webproof(server=aid.core.endpoint, seed=aid.agent_name)
    step = ExecutionStep(core=CoreStepRecord(
        request="test prompt", response="test response", proof=wp,
    ))
    return CompositeProof(aid_hash=aid.agent_hash, steps=[step])


def make_chain(aid, n_steps=1):
    chain = ExecutionChain(chain_id=aid.agent_hash)
    for i in range(n_steps):
        chain.append(ChainedStep(
            step_index=i, timestamp=1000.0 + i * 10,
            prev_hash=None, notary_commitments=[f"sha256:commit_{aid.agent_name}_{i}"],
        ))
    return chain


def make_policy(**overrides):
    defaults = {
        "allowed_endpoints": ["api.coingecko.com", "api.dex.io"],
        "allowed_models": ["claude-haiku-4.5"],
        "valid_until": time.time() + 86400,
        "max_delegation_depth": 1,
        "max_api_calls": 10,
    }
    defaults.update(overrides)
    return DelegationPolicy(**defaults)


def make_valid_2agent_chain():
    """Build a valid 2-agent delegation chain for attack injection."""
    aid_root = make_aid(name="RootAgent")
    aid_sub = make_aid(name="SubAgent", endpoint="api.coingecko.com")
    root_chain = make_chain(aid_root, n_steps=2)
    grant = DelegationGrant(
        delegator_aid_hash=aid_root.agent_hash,
        delegatee_aid_hash=aid_sub.agent_hash,
        policy=make_policy(),
        issuance_timestamp=1200.0,
        notary_attestation_ref=root_chain.steps[-1].notary_commitments[0],
    ).seal()
    step = DelegationExecutionStep(grant=grant, agent_proof=make_composite(aid_sub))
    chain = DelegationExecutionChain(
        root_aid_hash=aid_root.agent_hash,
        root_chain=root_chain,
        delegation_tree=[step],
    )
    aid_registry = {aid_root.agent_hash: aid_root, aid_sub.agent_hash: aid_sub}
    return aid_root, aid_sub, chain, aid_registry


# ============================================================
# 8 Attack Scenarios
# ============================================================

class TestA1_DelegationReplacement:
    """A1: Host replaces delegation target (SubAgent -> EvilAgent)."""
    def test(self):
        aid_root, aid_sub, chain, aid_registry = make_valid_2agent_chain()
        chain.delegation_tree[0].grant.delegatee_aid_hash = "sha256:evil_agent"
        verifier = DelegationVerifier(root_aid=aid_root)
        result = verifier.verify_chain(chain, aid_registry)
        assert result.authentic is False
        assert result.fault_type == "grant_tampered"


class TestA2_SubResultForgery:
    """A2: Host forges sub-agent execution result."""
    def test(self):
        aid_root, aid_sub, chain, aid_registry = make_valid_2agent_chain()
        aid_wrong = make_aid(name="WrongAgent", endpoint="api.evil.com")
        chain.delegation_tree[0].agent_proof = make_composite(aid_wrong)
        verifier = DelegationVerifier(root_aid=aid_root)
        result = verifier.verify_chain(chain, aid_registry)
        assert result.authentic is False
        assert result.fault_type == "subagent_proof_invalid"


class TestA3_DepthViolation:
    """A3: Sub-agent delegates further despite max_depth=0."""
    def test(self):
        aid_root, aid_sub, chain, aid_registry = make_valid_2agent_chain()
        chain.delegation_tree[0].grant.policy.max_delegation_depth = 0
        chain.delegation_tree[0].grant = chain.delegation_tree[0].grant.seal()
        aid_c = make_aid(name="AgentC", endpoint="api.c.com")
        inner_grant = DelegationGrant(
            delegator_aid_hash=aid_sub.agent_hash,
            delegatee_aid_hash=aid_c.agent_hash,
            policy=make_policy(max_delegation_depth=0),
            issuance_timestamp=1300.0, notary_attestation_ref="sha256:fake",
        ).seal()
        chain.delegation_tree[0].sub_delegations.append(
            DelegationExecutionStep(grant=inner_grant, agent_proof=make_composite(aid_c))
        )
        aid_registry[aid_c.agent_hash] = aid_c
        verifier = DelegationVerifier(root_aid=aid_root)
        result = verifier.verify_chain(chain, aid_registry)
        assert result.authentic is False
        assert result.fault_type == "policy_violation"


class TestA4_APICallOverrun:
    """A4: Sub-agent exceeds max_api_calls limit."""
    def test(self):
        _, aid_sub, chain, aid_registry = make_valid_2agent_chain()
        chain.delegation_tree[0].grant.policy.max_api_calls = 1
        chain.delegation_tree[0].grant = chain.delegation_tree[0].grant.seal()
        wp1 = make_webproof(seed="call1")
        wp2 = make_webproof(seed="call2")
        wp3 = make_webproof(seed="call3")
        step = ExecutionStep(
            core=CoreStepRecord(request="test", response="test", proof=wp1),
            tools=[
                ToolCallRecord(tool_name="API1", request="r1", response="r1", proof=wp2),
                ToolCallRecord(tool_name="API2", request="r2", response="r2", proof=wp3),
            ],
        )
        aid_sub2 = make_aid(name="SubAgent", endpoint="api.coingecko.com")
        chain.delegation_tree[0].agent_proof = CompositeProof(
            aid_hash=aid_sub2.agent_hash, steps=[step],
        )
        aid_registry[aid_sub2.agent_hash] = aid_sub2
        aid_root = aid_registry[list(aid_registry.keys())[0]]
        verifier = DelegationVerifier(root_aid=aid_root)
        result = verifier.verify_chain(chain, aid_registry)
        assert result.authentic is False
        assert result.fault_type == "policy_violation"


class TestA6_ExpiredGrant:
    """A6: Delegation grant has expired."""
    def test(self):
        _, _, chain, aid_registry = make_valid_2agent_chain()
        chain.delegation_tree[0].grant.policy.valid_until = 100.0
        chain.delegation_tree[0].grant = chain.delegation_tree[0].grant.seal()
        aid_root = aid_registry[list(aid_registry.keys())[0]]
        verifier = DelegationVerifier(root_aid=aid_root)
        result = verifier.verify_chain(chain, aid_registry)
        assert result.authentic is False
        assert result.fault_type == "expired"


class TestA7_GrantTampering:
    """A7: Host modifies policy field without resealing."""
    def test(self):
        _, _, chain, aid_registry = make_valid_2agent_chain()
        chain.delegation_tree[0].grant.policy.max_api_calls = 999
        aid_root = aid_registry[list(aid_registry.keys())[0]]
        verifier = DelegationVerifier(root_aid=aid_root)
        result = verifier.verify_chain(chain, aid_registry)
        assert result.authentic is False
        assert result.fault_type == "grant_tampered"


class TestA8_Collusion:
    """A8: Host0 and Host1 collude -- valid grant + fake proof."""
    def test(self):
        _, _, chain, aid_registry = make_valid_2agent_chain()
        aid_evil = make_aid(name="EvilAgent", endpoint="api.evil.com")
        chain.delegation_tree[0].agent_proof = make_composite(aid_evil)
        aid_root = aid_registry[list(aid_registry.keys())[0]]
        verifier = DelegationVerifier(root_aid=aid_root)
        result = verifier.verify_chain(chain, aid_registry)
        assert result.authentic is False
        assert result.fault_type == "subagent_proof_invalid"


class TestA10_ReplayedGrant:
    """A10: Host replays old grant -- detected as expired."""
    def test(self):
        _, _, chain, aid_registry = make_valid_2agent_chain()
        chain.delegation_tree[0].grant.policy.valid_until = 1300.0
        chain.delegation_tree[0].grant = chain.delegation_tree[0].grant.seal()
        aid_root = aid_registry[list(aid_registry.keys())[0]]
        verifier = DelegationVerifier(root_aid=aid_root)
        result = verifier.verify_chain(chain, aid_registry)
        assert result.authentic is False
        assert result.fault_type == "expired"
