# tests/test_delegation.py
"""Unit tests for delegation data structures."""
import pytest
import time
from vet_core.aid import AID, ComponentSpec, VerificationMeta
from vet_core.compositional import CompositeProof, ExecutionStep, CoreStepRecord
from vet_webproofs.prover import WebProof
from vet_core.execution_chain import ExecutionChain, ChainedStep
from vet_core.delegation import (
    DelegationPolicy, DelegationGrant,
    DelegationExecutionStep, DelegationExecutionChain,
)


class TestDelegationPolicy:
    def test_policy_creation_defaults(self):
        p = DelegationPolicy(
            allowed_endpoints=["api.coingecko.com"],
            allowed_models=["gpt-4o"],
            valid_until=time.time() + 3600,
        )
        assert p.max_delegation_depth == 0
        assert p.max_api_calls == 10
        assert p.required_aid_hash is None

    def test_policy_creation_full(self):
        p = DelegationPolicy(
            allowed_endpoints=["api.coingecko.com", "api.dex.io"],
            allowed_models=["gpt-4o", "claude-haiku-4.5"],
            valid_until=time.time() + 7200,
            max_delegation_depth=1,
            max_api_calls=5,
            required_aid_hash="sha256:abc123",
        )
        assert p.max_delegation_depth == 1
        assert p.max_api_calls == 5
        assert p.required_aid_hash == "sha256:abc123"

    def test_policy_rejects_empty_endpoints(self):
        with pytest.raises(ValueError, match="allowed_endpoints cannot be empty"):
            DelegationPolicy(
                allowed_endpoints=[],
                allowed_models=["gpt-4o"],
                valid_until=time.time() + 3600,
            )

    def test_policy_rejects_empty_models(self):
        with pytest.raises(ValueError, match="allowed_models cannot be empty"):
            DelegationPolicy(
                allowed_endpoints=["api.coingecko.com"],
                allowed_models=[],
                valid_until=time.time() + 3600,
            )


class TestDelegationGrant:
    def make_policy(self):
        return DelegationPolicy(
            allowed_endpoints=["api.coingecko.com"],
            allowed_models=["gpt-4o"],
            valid_until=time.time() + 3600,
        )

    def test_grant_creation_and_seal(self):
        g = DelegationGrant(
            delegator_aid_hash="sha256:aaa111",
            delegatee_aid_hash="sha256:bbb222",
            policy=self.make_policy(),
            issuance_timestamp=time.time(),
            notary_attestation_ref="sha256:notary_commitment_xyz",
        ).seal()

        assert g.grant_hash.startswith("sha256:")
        assert len(g.grant_hash) == 71  # "sha256:" + 64 hex chars

    def test_grant_hash_deterministic(self):
        policy = self.make_policy()
        g1 = DelegationGrant(
            delegator_aid_hash="sha256:aaa111",
            delegatee_aid_hash="sha256:bbb222",
            policy=policy,
            issuance_timestamp=1000000.0,
            notary_attestation_ref="sha256:ref1",
        ).seal()

        g2 = DelegationGrant(
            delegator_aid_hash="sha256:aaa111",
            delegatee_aid_hash="sha256:bbb222",
            policy=policy,
            issuance_timestamp=1000000.0,
            notary_attestation_ref="sha256:ref1",
        ).seal()

        assert g1.grant_hash == g2.grant_hash

    def test_grant_hash_changes_on_policy_change(self):
        p1 = self.make_policy()
        p2 = DelegationPolicy(
            allowed_endpoints=["api.coingecko.com"],
            allowed_models=["gpt-4o"],
            valid_until=time.time() + 3600,
            max_api_calls=99,
        )

        g1 = DelegationGrant(
            delegator_aid_hash="sha256:aaa111",
            delegatee_aid_hash="sha256:bbb222",
            policy=p1,
            issuance_timestamp=1000000.0,
            notary_attestation_ref="sha256:ref1",
        ).seal()

        g2 = DelegationGrant(
            delegator_aid_hash="sha256:aaa111",
            delegatee_aid_hash="sha256:bbb222",
            policy=p2,
            issuance_timestamp=1000000.0,
            notary_attestation_ref="sha256:ref1",
        ).seal()

        assert g1.grant_hash != g2.grant_hash

    def test_grant_hash_changes_on_delegatee_change(self):
        policy = self.make_policy()
        g1 = DelegationGrant(
            delegator_aid_hash="sha256:aaa111",
            delegatee_aid_hash="sha256:bbb222",
            policy=policy,
            issuance_timestamp=1000000.0,
            notary_attestation_ref="sha256:ref1",
        ).seal()

        g2 = DelegationGrant(
            delegator_aid_hash="sha256:aaa111",
            delegatee_aid_hash="sha256:ccc333",
            policy=policy,
            issuance_timestamp=1000000.0,
            notary_attestation_ref="sha256:ref1",
        ).seal()

        assert g1.grant_hash != g2.grant_hash

    def test_grant_parent_chain(self):
        policy = self.make_policy()
        parent = DelegationGrant(
            delegator_aid_hash="sha256:root",
            delegatee_aid_hash="sha256:aaa111",
            policy=policy,
            issuance_timestamp=1000000.0,
            notary_attestation_ref="sha256:ref_parent",
        ).seal()

        child = DelegationGrant(
            delegator_aid_hash="sha256:aaa111",
            delegatee_aid_hash="sha256:bbb222",
            policy=policy,
            parent_grant_hash=parent.grant_hash,
            issuance_timestamp=1000100.0,
            notary_attestation_ref="sha256:ref_child",
        ).seal()

        assert child.parent_grant_hash == parent.grant_hash
        assert child.grant_hash != parent.grant_hash


# ---------------------------------------------------------------------------
# Mock helpers for DelegationExecutionStep and DelegationExecutionChain tests
# ---------------------------------------------------------------------------

def make_mock_webproof(server="api.openai.com", commitment=None):
    import hashlib
    if commitment is None:
        commitment = "sha256:" + hashlib.sha256(b"mock_transcript").hexdigest()
    return WebProof(
        server_name=server,
        attestation={
            "body": {"server_name": server, "transcript_commitment": commitment},
            "signature": "0xabcd",
        },
        transcript_sent=b"GET / HTTP/1.1\r\n\r\n",
        transcript_recv=b"HTTP/1.1 200 OK\r\n\r\n{}",
        commitment=commitment,
    )


def make_mock_aid(name="TestAgent", endpoint="api.openai.com"):
    return AID(
        agent_name=name,
        core=ComponentSpec(
            name="Core",
            endpoint=endpoint,
            injection_algorithm_uid="sha256:inject001",
            parsing_algorithm_uid="sha256:parse001",
        ),
    ).seal()


def make_mock_composite(aid):
    wp = make_mock_webproof(server=aid.core.endpoint)
    step = ExecutionStep(
        core=CoreStepRecord(
            request="test prompt",
            response="test response",
            proof=wp,
        )
    )
    return CompositeProof(aid_hash=aid.agent_hash, steps=[step])


# ---------------------------------------------------------------------------
# DelegationExecutionStep tests
# ---------------------------------------------------------------------------

class TestDelegationExecutionStep:
    def make_policy(self):
        return DelegationPolicy(
            allowed_endpoints=["api.coingecko.com"],
            allowed_models=["claude-haiku-4.5"],
            valid_until=time.time() + 3600,
        )

    def test_step_creation(self):
        aid_delegatee = make_mock_aid(name="SubAgent", endpoint="api.coingecko.com")
        policy = self.make_policy()
        grant = DelegationGrant(
            delegator_aid_hash="sha256:root_aid",
            delegatee_aid_hash=aid_delegatee.agent_hash,
            policy=policy,
            issuance_timestamp=time.time(),
            notary_attestation_ref="sha256:attest_ref_1",
        ).seal()

        composite = make_mock_composite(aid_delegatee)
        step = DelegationExecutionStep(
            grant=grant,
            agent_proof=composite,
        )

        assert step.grant.grant_hash == grant.grant_hash
        assert step.agent_proof.aid_hash == aid_delegatee.agent_hash
        assert step.sub_delegations == []

    def test_step_with_sub_delegations(self):
        aid_b = make_mock_aid(name="AgentB")
        aid_c = make_mock_aid(name="AgentC")
        policy_ab = self.make_policy()
        policy_bc = DelegationPolicy(
            allowed_endpoints=["api.dex.io"],
            allowed_models=["gpt-4o"],
            valid_until=time.time() + 3600,
            max_delegation_depth=0,
        )

        grant_ab = DelegationGrant(
            delegator_aid_hash="sha256:root",
            delegatee_aid_hash=aid_b.agent_hash,
            policy=policy_ab,
            issuance_timestamp=time.time(),
            notary_attestation_ref="sha256:ref_ab",
        ).seal()

        grant_bc = DelegationGrant(
            delegator_aid_hash=aid_b.agent_hash,
            delegatee_aid_hash=aid_c.agent_hash,
            policy=policy_bc,
            parent_grant_hash=grant_ab.grant_hash,
            issuance_timestamp=time.time(),
            notary_attestation_ref="sha256:ref_bc",
        ).seal()

        step_c = DelegationExecutionStep(
            grant=grant_bc,
            agent_proof=make_mock_composite(aid_c),
        )
        step_b = DelegationExecutionStep(
            grant=grant_ab,
            agent_proof=make_mock_composite(aid_b),
            sub_delegations=[step_c],
        )

        assert len(step_b.sub_delegations) == 1
        assert step_b.sub_delegations[0].grant.delegatee_aid_hash == aid_c.agent_hash


# ---------------------------------------------------------------------------
# DelegationExecutionChain tests
# ---------------------------------------------------------------------------

class TestDelegationExecutionChain:
    def make_policy(self, depth=1):
        return DelegationPolicy(
            allowed_endpoints=["api.coingecko.com"],
            allowed_models=["claude-haiku-4.5"],
            valid_until=time.time() + 3600,
            max_delegation_depth=depth,
        )

    def test_chain_creation_empty(self):
        root_aid = make_mock_aid(name="RootAgent")
        root_chain = ExecutionChain(chain_id=root_aid.agent_hash)
        root_chain.append(ChainedStep(
            step_index=0,
            timestamp=time.time(),
            prev_hash=None,
            notary_commitments=["sha256:commit_root"],
        ))

        chain = DelegationExecutionChain(
            root_aid_hash=root_aid.agent_hash,
            root_chain=root_chain,
        )

        assert chain.total_agents == 1
        assert chain.max_depth == 0
        assert chain.flatten() == []

    def test_chain_with_one_delegation(self):
        root_aid = make_mock_aid(name="RootAgent")
        sub_aid = make_mock_aid(name="SubAgent", endpoint="api.coingecko.com")

        root_chain = ExecutionChain(chain_id=root_aid.agent_hash)
        root_chain.append(ChainedStep(
            step_index=0, timestamp=time.time(),
            prev_hash=None, notary_commitments=["sha256:r1"],
        ))

        policy = self.make_policy()
        grant = DelegationGrant(
            delegator_aid_hash=root_aid.agent_hash,
            delegatee_aid_hash=sub_aid.agent_hash,
            policy=policy,
            issuance_timestamp=time.time(),
            notary_attestation_ref="sha256:attest_deleg",
        ).seal()

        step = DelegationExecutionStep(
            grant=grant,
            agent_proof=make_mock_composite(sub_aid),
        )

        chain = DelegationExecutionChain(
            root_aid_hash=root_aid.agent_hash,
            root_chain=root_chain,
            delegation_tree=[step],
        )

        assert chain.total_agents == 2
        assert chain.max_depth == 1

    def test_chain_with_nested_delegation(self):
        """3-level delegation: Root -> B -> C"""
        aid_root = make_mock_aid(name="Root")
        aid_b = make_mock_aid(name="AgentB", endpoint="api.b.com")
        aid_c = make_mock_aid(name="AgentC", endpoint="api.c.com")

        root_chain = ExecutionChain(chain_id=aid_root.agent_hash)
        root_chain.append(ChainedStep(
            step_index=0, timestamp=1000.0,
            prev_hash=None, notary_commitments=["sha256:r1"],
        ))

        p_root_b = self.make_policy(depth=1)
        grant_rb = DelegationGrant(
            delegator_aid_hash=aid_root.agent_hash,
            delegatee_aid_hash=aid_b.agent_hash,
            policy=p_root_b,
            issuance_timestamp=1000.0,
            notary_attestation_ref="sha256:ref_rb",
        ).seal()

        p_b_c = DelegationPolicy(
            allowed_endpoints=["api.c.com"],
            allowed_models=["gpt-4o"],
            valid_until=time.time() + 3600,
            max_delegation_depth=0,
        )
        grant_bc = DelegationGrant(
            delegator_aid_hash=aid_b.agent_hash,
            delegatee_aid_hash=aid_c.agent_hash,
            policy=p_b_c,
            parent_grant_hash=grant_rb.grant_hash,
            issuance_timestamp=1100.0,
            notary_attestation_ref="sha256:ref_bc",
        ).seal()

        step_c = DelegationExecutionStep(
            grant=grant_bc,
            agent_proof=make_mock_composite(aid_c),
        )
        step_b = DelegationExecutionStep(
            grant=grant_rb,
            agent_proof=make_mock_composite(aid_b),
            sub_delegations=[step_c],
        )

        chain = DelegationExecutionChain(
            root_aid_hash=aid_root.agent_hash,
            root_chain=root_chain,
            delegation_tree=[step_b],
        )

        assert chain.total_agents == 3
        assert chain.max_depth == 2
        flattened = chain.flatten()
        assert len(flattened) == 2


# ---------------------------------------------------------------------------
# DelegationVerifier tests
# ---------------------------------------------------------------------------

from vet_core.delegation_verifier import DelegationVerifier, DelegationVerificationResult


class TestDelegationVerifier:
    def make_policy(self, depth=1):
        return DelegationPolicy(
            allowed_endpoints=["api.coingecko.com"],
            allowed_models=["claude-haiku-4.5"],
            valid_until=time.time() + 3600,
            max_delegation_depth=depth,
        )

    def test_verify_valid_single_delegation(self):
        """A valid 2-agent delegation should pass verification."""
        aid_root = make_mock_aid(name="Root")
        aid_sub = make_mock_aid(name="SubAgent", endpoint="api.coingecko.com")

        root_chain = ExecutionChain(chain_id=aid_root.agent_hash)
        root_chain.append(ChainedStep(
            step_index=0, timestamp=time.time(),
            prev_hash=None, notary_commitments=["sha256:r1"],
        ))

        grant = DelegationGrant(
            delegator_aid_hash=aid_root.agent_hash,
            delegatee_aid_hash=aid_sub.agent_hash,
            policy=self.make_policy(),
            issuance_timestamp=time.time(),
            notary_attestation_ref="sha256:ref1",
        ).seal()

        step = DelegationExecutionStep(
            grant=grant,
            agent_proof=make_mock_composite(aid_sub),
        )
        chain = DelegationExecutionChain(
            root_aid_hash=aid_root.agent_hash,
            root_chain=root_chain,
            delegation_tree=[step],
        )

        verifier = DelegationVerifier(root_aid=aid_root)
        all_aids = {aid_root.agent_hash: aid_root, aid_sub.agent_hash: aid_sub}
        result = verifier.verify_chain(chain, all_aids)

        assert result.authentic is True
        assert result.total_agents == 2
        assert result.chain_depth == 1

    def test_verify_rejects_wrong_root_aid(self):
        """Verifier holds AID_A but chain claims root is AID_B."""
        aid_v = make_mock_aid(name="VerifierAID")
        aid_chain = make_mock_aid(name="ChainAID")

        root_chain = ExecutionChain(chain_id=aid_chain.agent_hash)
        root_chain.append(ChainedStep(
            step_index=0, timestamp=time.time(),
            prev_hash=None, notary_commitments=["sha256:r1"],
        ))

        chain = DelegationExecutionChain(
            root_aid_hash=aid_chain.agent_hash,
            root_chain=root_chain,
        )

        verifier = DelegationVerifier(root_aid=aid_v)
        result = verifier.verify_chain(chain, {})

        assert result.authentic is False
        assert "根身份哈希不匹配" in result.error
        assert result.blame_path == ["root"]

    def test_verify_rejects_tampered_grant(self):
        """Grant hash doesn't match -- host tampered with the grant."""
        aid_root = make_mock_aid(name="Root")
        aid_sub = make_mock_aid(name="SubAgent", endpoint="api.coingecko.com")

        root_chain = ExecutionChain(chain_id=aid_root.agent_hash)
        root_chain.append(ChainedStep(
            step_index=0, timestamp=time.time(),
            prev_hash=None, notary_commitments=["sha256:r1"],
        ))

        grant = DelegationGrant(
            delegator_aid_hash=aid_root.agent_hash,
            delegatee_aid_hash=aid_sub.agent_hash,
            policy=self.make_policy(),
            issuance_timestamp=time.time(),
            notary_attestation_ref="sha256:ref1",
        ).seal()

        # Tamper: change delegatee without recomputing hash
        grant.delegatee_aid_hash = "sha256:evil_agent"

        step = DelegationExecutionStep(
            grant=grant,
            agent_proof=make_mock_composite(aid_sub),
        )
        chain = DelegationExecutionChain(
            root_aid_hash=aid_root.agent_hash,
            root_chain=root_chain,
            delegation_tree=[step],
        )

        verifier = DelegationVerifier(root_aid=aid_root)
        result = verifier.verify_chain(chain, {})

        assert result.authentic is False
        assert result.fault_type == "grant_tampered"

    def test_verify_rejects_expired_grant(self):
        """Delegation grant has expired."""
        aid_root = make_mock_aid(name="Root")
        aid_sub = make_mock_aid(name="SubAgent", endpoint="api.coingecko.com")

        root_chain = ExecutionChain(chain_id=aid_root.agent_hash)
        root_chain.append(ChainedStep(
            step_index=0, timestamp=time.time(),
            prev_hash=None, notary_commitments=["sha256:r1"],
        ))

        grant = DelegationGrant(
            delegator_aid_hash=aid_root.agent_hash,
            delegatee_aid_hash=aid_sub.agent_hash,
            policy=DelegationPolicy(
                allowed_endpoints=["api.coingecko.com"],
                allowed_models=["gpt-4o"],
                valid_until=100.0,  # expired long ago
            ),
            issuance_timestamp=50.0,
            notary_attestation_ref="sha256:ref1",
        ).seal()

        step = DelegationExecutionStep(
            grant=grant,
            agent_proof=make_mock_composite(aid_sub),
        )
        chain = DelegationExecutionChain(
            root_aid_hash=aid_root.agent_hash,
            root_chain=root_chain,
            delegation_tree=[step],
        )

        verifier = DelegationVerifier(root_aid=aid_root)
        result = verifier.verify_chain(chain, {aid_sub.agent_hash: aid_sub})

        assert result.authentic is False
        assert result.fault_type == "expired"

    def test_verify_rejects_policy_violation_depth(self):
        """Sub-agent delegates further but policy says max_depth=0."""
        aid_root = make_mock_aid(name="Root")
        aid_b = make_mock_aid(name="AgentB", endpoint="api.b.com")
        aid_c = make_mock_aid(name="AgentC", endpoint="api.c.com")

        root_chain = ExecutionChain(chain_id=aid_root.agent_hash)
        root_chain.append(ChainedStep(
            step_index=0, timestamp=time.time(),
            prev_hash=None, notary_commitments=["sha256:r1"],
        ))

        grant_rb = DelegationGrant(
            delegator_aid_hash=aid_root.agent_hash,
            delegatee_aid_hash=aid_b.agent_hash,
            policy=DelegationPolicy(
                allowed_endpoints=["api.b.com"],
                allowed_models=["gpt-4o"],
                valid_until=time.time() + 3600,
                max_delegation_depth=0,
            ),
            issuance_timestamp=time.time(),
            notary_attestation_ref="sha256:ref_rb",
        ).seal()

        grant_bc = DelegationGrant(
            delegator_aid_hash=aid_b.agent_hash,
            delegatee_aid_hash=aid_c.agent_hash,
            policy=DelegationPolicy(
                allowed_endpoints=["api.c.com"],
                allowed_models=["gpt-4o"],
                valid_until=time.time() + 3600,
            ),
            parent_grant_hash=grant_rb.grant_hash,
            issuance_timestamp=time.time(),
            notary_attestation_ref="sha256:ref_bc",
        ).seal()

        step_b = DelegationExecutionStep(
            grant=grant_rb,
            agent_proof=make_mock_composite(aid_b),
            sub_delegations=[
                DelegationExecutionStep(
                    grant=grant_bc,
                    agent_proof=make_mock_composite(aid_c),
                )
            ],
        )
        chain = DelegationExecutionChain(
            root_aid_hash=aid_root.agent_hash,
            root_chain=root_chain,
            delegation_tree=[step_b],
        )

        all_aids = {
            aid_root.agent_hash: aid_root,
            aid_b.agent_hash: aid_b,
            aid_c.agent_hash: aid_c,
        }
        verifier = DelegationVerifier(root_aid=aid_root)
        result = verifier.verify_chain(chain, all_aids)

        assert result.authentic is False
        assert result.fault_type == "policy_violation"

    def test_blame_attribution_nested(self):
        """In a 3-level chain where C's proof is invalid, blame C's host."""
        aid_root = make_mock_aid(name="Root")
        aid_b = make_mock_aid(name="AgentB", endpoint="api.b.com")
        aid_c = make_mock_aid(name="AgentC", endpoint="api.c.com")

        root_chain = ExecutionChain(chain_id=aid_root.agent_hash)
        root_chain.append(ChainedStep(
            step_index=0, timestamp=time.time(),
            prev_hash=None, notary_commitments=["sha256:r1"],
        ))

        grant_rb = DelegationGrant(
            delegator_aid_hash=aid_root.agent_hash,
            delegatee_aid_hash=aid_b.agent_hash,
            policy=self.make_policy(depth=1),
            issuance_timestamp=time.time(),
            notary_attestation_ref="sha256:ref_rb",
        ).seal()

        grant_bc = DelegationGrant(
            delegator_aid_hash=aid_b.agent_hash,
            delegatee_aid_hash=aid_c.agent_hash,
            policy=DelegationPolicy(
                allowed_endpoints=["api.c.com"],
                allowed_models=["gpt-4o"],
                valid_until=time.time() + 3600,
            ),
            parent_grant_hash=grant_rb.grant_hash,
            issuance_timestamp=time.time(),
            notary_attestation_ref="sha256:ref_bc",
        ).seal()

        # C's proof uses a different AID hash -> verification will fail
        aid_wrong = make_mock_aid(name="WrongAgent", endpoint="api.wrong.com")
        step_c = DelegationExecutionStep(
            grant=grant_bc,
            agent_proof=make_mock_composite(aid_wrong),
        )
        step_b = DelegationExecutionStep(
            grant=grant_rb,
            agent_proof=make_mock_composite(aid_b),
            sub_delegations=[step_c],
        )
        chain = DelegationExecutionChain(
            root_aid_hash=aid_root.agent_hash,
            root_chain=root_chain,
            delegation_tree=[step_b],
        )

        all_aids = {
            aid_root.agent_hash: aid_root,
            aid_b.agent_hash: aid_b,
            aid_c.agent_hash: aid_c,
        }
        verifier = DelegationVerifier(root_aid=aid_root)
        result = verifier.verify_chain(chain, all_aids)

        assert result.authentic is False
        assert result.fault_type == "subagent_proof_invalid"
        assert "AgentC" in str(result.blame_path) or len(result.blame_path) > 1


# ---------------------------------------------------------------------------
# Trading DAO Scenario Integration Tests
# ---------------------------------------------------------------------------

class TestTradingDAOScenario:
    def test_scenario_builds_valid_chain(self):
        from veritrade.multi_agent_scenario import TradingDAOScenario
        from vet_core.delegation_verifier import DelegationVerifier

        scenario = TradingDAOScenario()
        chain, aid_registry = scenario.build()

        assert chain.total_agents == 3
        assert chain.max_depth == 1
        assert len(chain.delegation_tree) == 2

        verifier = DelegationVerifier(root_aid=scenario.strategy_aid)
        result = verifier.verify_chain(chain, aid_registry)

        assert result.authentic is True, f"Scenario failed: {result.blame_attribution}"
        assert result.total_agents == 3
        assert result.chain_depth == 1

    def test_scenario_detects_tampered_execution(self):
        from veritrade.multi_agent_scenario import TradingDAOScenario
        from vet_core.delegation_verifier import DelegationVerifier

        scenario = TradingDAOScenario()
        chain, aid_registry = scenario.build()

        # Tamper: change ETH executor's AID hash in the grant without resealing
        chain.delegation_tree[0].grant.delegatee_aid_hash = "sha256:evil_executor"

        verifier = DelegationVerifier(root_aid=scenario.strategy_aid)
        result = verifier.verify_chain(chain, aid_registry)

        assert result.authentic is False
        assert result.fault_type == "grant_tampered"
