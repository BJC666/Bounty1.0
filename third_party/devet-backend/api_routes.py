"""DeVET API Routes — wraps existing vet-repro modules as REST endpoints."""
import sys
import os
import time
import statistics
import json
import hashlib

# Ensure vet-repro is importable
VET_REPRO = os.path.join(os.path.dirname(__file__), "..", "vet-repro")
if VET_REPRO not in sys.path:
    sys.path.insert(0, VET_REPRO)

from fastapi import APIRouter, HTTPException
from pydantic import BaseModel
from typing import Optional

from vet_core.aid import AID, ComponentSpec
from vet_core.compositional import CompositeProof, ExecutionStep, CoreStepRecord, ToolCallRecord
from vet_core.execution_chain import ExecutionChain, ChainedStep
from vet_webproofs.prover import WebProof
from vet_core.delegation import (
    DelegationPolicy,
    DelegationGrant,
    DelegationExecutionStep,
    DelegationExecutionChain,
)
from vet_core.delegation_verifier import DelegationVerifier, DelegationVerificationResult
from veritrade.multi_agent_scenario import TradingDAOScenario

router = APIRouter()

# ---------------------------------------------------------------------------
# State: store the last-built chain in memory for verification
# ---------------------------------------------------------------------------
_scenario: Optional[TradingDAOScenario] = None
_chain: Optional[DelegationExecutionChain] = None
_aid_registry: dict = {}

# ---------------------------------------------------------------------------
# Attack definitions
# ---------------------------------------------------------------------------
ATTACKS = {
    "A1_delegation_replacement": {
        "name": "A1: 委托替换",
        "description": "Host 把委托目标从诚实 Agent 替换为恶意 Agent",
        "expected_fault": "grant_tampered",
    },
    "A2_sub_result_forgery": {
        "name": "A2: 子结果伪造",
        "description": "Host 伪造子 Agent 的执行结果",
        "expected_fault": "subagent_proof_invalid",
    },
    "A3_depth_violation": {
        "name": "A3: 委托深度违规",
        "description": "子 Agent 继续委托但策略禁止(max_depth=0)",
        "expected_fault": "policy_violation",
    },
    "A4_api_overrun": {
        "name": "A4: API 调用超限",
        "description": "子 Agent 的 API 调用次数超过策略限制",
        "expected_fault": "policy_violation",
    },
    "A6_expired_grant": {
        "name": "A6: 委托过期",
        "description": "使用已过期的委托授权",
        "expected_fault": "expired",
    },
    "A7_grant_tampering": {
        "name": "A7: Grant 篡改",
        "description": "Host 修改 policy 字段但不重新 seal",
        "expected_fault": "grant_tampered",
    },
    "A8_collusion": {
        "name": "A8: 跨 Host 共谋",
        "description": "Host_A 和 Host_B 协作伪造证明",
        "expected_fault": "subagent_proof_invalid",
    },
    "A10_replayed_grant": {
        "name": "A10: 重放攻击",
        "description": "Host 重放已过期的旧委托",
        "expected_fault": "expired",
    },
}

# ---------------------------------------------------------------------------
# Serialization helpers
# ---------------------------------------------------------------------------

def _make_aid(name: str, endpoint: str) -> AID:
    return AID(
        agent_name=name,
        core=ComponentSpec(
            name="Core", endpoint=endpoint,
            injection_algorithm_uid=f"sha256:inject_{name}",
            parsing_algorithm_uid=f"sha256:parse_{name}",
        ),
    ).seal()


def _make_webproof(server: str = "api.openai.com", seed: str = "test") -> WebProof:
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


def _make_composite(aid: AID) -> CompositeProof:
    wp = _make_webproof(server=aid.core.endpoint, seed=aid.agent_name)
    step = ExecutionStep(core=CoreStepRecord(
        request="test prompt", response="test response", proof=wp,
    ))
    return CompositeProof(aid_hash=aid.agent_hash, steps=[step])


def _serialize_chain(chain: DelegationExecutionChain) -> dict:
    """Serialize a delegation chain to JSON-safe dict."""
    def _serialize_step(step: DelegationExecutionStep) -> dict:
        grant = step.grant
        return {
            "agent_aid_hash": step.agent_aid_hash,
            "delegator_hash": grant.delegator_aid_hash[:24] + "...",
            "delegatee_hash": grant.delegatee_aid_hash[:24] + "...",
            "grant_hash": grant.grant_hash[:24] + "...",
            "policy": {
                "allowed_endpoints": grant.policy.allowed_endpoints,
                "allowed_models": grant.policy.allowed_models,
                "valid_until": grant.policy.valid_until,
                "max_delegation_depth": grant.policy.max_delegation_depth,
                "max_api_calls": grant.policy.max_api_calls,
                "required_aid_hash": grant.policy.required_aid_hash,
                "is_expired": grant.policy.is_expired(),
            },
            "sub_proofs": len(step.agent_proof.all_sub_proofs()),
            "sub_delegations": [_serialize_step(s) for s in step.sub_delegations],
        }

    return {
        "root_aid_hash": chain.root_aid_hash[:24] + "...",
        "total_agents": chain.total_agents,
        "max_depth": chain.max_depth,
        "grant_count": len(chain.collect_all_grants()),
        "root_chain_steps": len(chain.root_chain.steps),
        "delegation_tree": [_serialize_step(s) for s in chain.delegation_tree],
    }


def _serialize_result(result: DelegationVerificationResult) -> dict:
    """Serialize verification result to JSON-safe dict."""
    return {
        "authentic": result.authentic,
        "blame_path": result.blame_path,
        "error": result.error,
        "fault_type": result.fault_type,
        "chain_depth": result.chain_depth,
        "total_agents": result.total_agents,
        "blame_attribution": result.blame_attribution,
        "findings": [
            {
                "agent": f.get("agent", "unknown")[:30],
                "authentic": f.get("authentic", None),
                "fault_type": f.get("fault_type", None),
                "chain_ok": f.get("chain_ok", None),
            }
            for f in result.findings
        ],
    }


# ---------------------------------------------------------------------------
# Routes
# ---------------------------------------------------------------------------

@router.get("/health")
def health():
    return {"status": "ok", "service": "DeVET System API"}


@router.post("/scenario/build")
def build_scenario():
    """Build a 3-agent Trading DAO delegation chain and store in memory."""
    global _scenario, _chain, _aid_registry

    _scenario = TradingDAOScenario()
    _chain, _aid_registry = _scenario.build()

    return {
        "status": "built",
        "chain": _serialize_chain(_chain),
    }


@router.post("/chain/verify")
def verify_chain():
    """Verify the currently stored delegation chain."""
    global _scenario, _chain, _aid_registry

    if _chain is None or _scenario is None:
        raise HTTPException(status_code=400, detail="No chain built. Call /api/scenario/build first.")

    verifier = DelegationVerifier(root_aid=_scenario.strategy_aid)
    result = verifier.verify_chain(_chain, _aid_registry)

    return {
        "status": "verified",
        "result": _serialize_result(result),
    }


@router.get("/attacks")
def list_attacks():
    """Return available attack types."""
    return {"attacks": ATTACKS}


@router.post("/attack/simulate")
def simulate_attack(req: dict):
    """Inject a specified attack into the chain and verify.

    Request body: {"attack_type": "A1_delegation_replacement"}
    """
    global _scenario, _chain, _aid_registry

    attack_type = req.get("attack_type", "")
    if attack_type not in ATTACKS:
        raise HTTPException(
            status_code=400,
            detail=f"Unknown attack type: {attack_type}. Available: {list(ATTACKS.keys())}"
        )

    attack_info = ATTACKS[attack_type]
    expected_fault = attack_info["expected_fault"]

    # Build a fresh chain and inject the attack
    scenario = TradingDAOScenario()
    chain, aid_registry = scenario.build()

    # Inject attack based on type
    if attack_type == "A1_delegation_replacement":
        chain.delegation_tree[0].grant.delegatee_aid_hash = "sha256:evil_agent"

    elif attack_type == "A2_sub_result_forgery":
        aid_wrong = _make_aid(name="WrongAgent", endpoint="api.evil.com")
        chain.delegation_tree[0].agent_proof = _make_composite(aid_wrong)

    elif attack_type == "A3_depth_violation":
        chain.delegation_tree[0].grant.policy.max_delegation_depth = 0
        chain.delegation_tree[0].grant = chain.delegation_tree[0].grant.seal()
        aid_c = _make_aid(name="AgentC", endpoint="api.c.com")
        inner_grant = DelegationGrant(
            delegator_aid_hash=chain.delegation_tree[0].grant.delegatee_aid_hash,
            delegatee_aid_hash=aid_c.agent_hash,
            policy=DelegationPolicy(
                allowed_endpoints=["api.c.com"], allowed_models=["gpt-4o"],
                valid_until=time.time() + 86400, max_delegation_depth=0,
            ),
            issuance_timestamp=time.time(), notary_attestation_ref="sha256:fake",
        ).seal()
        chain.delegation_tree[0].sub_delegations.append(
            DelegationExecutionStep(grant=inner_grant, agent_proof=_make_composite(aid_c))
        )
        aid_registry[aid_c.agent_hash] = aid_c

    elif attack_type == "A4_api_overrun":
        # Reduce policy limit to 1 API call, but give the agent 3 sub-proofs
        chain.delegation_tree[0].grant.policy.max_api_calls = 1
        chain.delegation_tree[0].grant = chain.delegation_tree[0].grant.seal()
        # Reuse the delegatee's AID hash so the AID check passes, but API limit fails
        delegatee_hash = chain.delegation_tree[0].grant.delegatee_aid_hash
        wp1 = _make_webproof(seed="c1")
        wp2 = _make_webproof(seed="c2")
        wp3 = _make_webproof(seed="c3")
        step = ExecutionStep(
            core=CoreStepRecord(request="t", response="t", proof=wp1),
            tools=[
                ToolCallRecord(tool_name="API1", request="r1", response="r1", proof=wp2),
                ToolCallRecord(tool_name="API2", request="r2", response="r2", proof=wp3),
            ],
        )
        chain.delegation_tree[0].agent_proof = CompositeProof(
            aid_hash=delegatee_hash, steps=[step],
        )

    elif attack_type == "A6_expired_grant":
        chain.delegation_tree[0].grant.policy.valid_until = 100.0
        chain.delegation_tree[0].grant = chain.delegation_tree[0].grant.seal()

    elif attack_type == "A7_grant_tampering":
        chain.delegation_tree[0].grant.policy.max_api_calls = 999

    elif attack_type == "A8_collusion":
        aid_evil = _make_aid(name="EvilAgent", endpoint="api.evil.com")
        chain.delegation_tree[0].agent_proof = _make_composite(aid_evil)

    elif attack_type == "A10_replayed_grant":
        chain.delegation_tree[0].grant.policy.valid_until = 1300.0
        chain.delegation_tree[0].grant = chain.delegation_tree[0].grant.seal()

    # Verify the attacked chain
    verifier = DelegationVerifier(root_aid=scenario.strategy_aid)
    result = verifier.verify_chain(chain, aid_registry)

    detected = (not result.authentic)
    fault_match = (result.fault_type == expected_fault) if detected else False

    return {
        "attack_type": attack_type,
        "attack_name": attack_info["name"],
        "attack_description": attack_info["description"],
        "expected_fault": expected_fault,
        "detected": detected,
        "fault_match": fault_match,
        "result": _serialize_result(result),
    }


@router.post("/benchmark/verify")
def benchmark_verify(req: dict = None):
    """Run verification latency benchmark.

    Request body (optional): {"runs": 50}
    """
    global _scenario, _chain, _aid_registry

    runs = (req or {}).get("runs", 50)

    scenario = TradingDAOScenario()
    chain, aid_registry = scenario.build()
    verifier = DelegationVerifier(root_aid=scenario.strategy_aid)

    latencies = []
    for _ in range(runs):
        t0 = time.perf_counter()
        result = verifier.verify_chain(chain, aid_registry)
        elapsed = time.perf_counter() - t0
        latencies.append(elapsed)

    # Also benchmark blame attribution
    chain.delegation_tree[0].grant.delegatee_aid_hash = "sha256:evil"
    blame_latencies = []
    for _ in range(runs):
        t0 = time.perf_counter()
        result = verifier.verify_chain(chain, aid_registry)
        elapsed = time.perf_counter() - t0
        blame_latencies.append(elapsed)

    return {
        "verification": {
            "runs": runs,
            "mean_ms": round(statistics.mean(latencies) * 1000, 3),
            "median_ms": round(statistics.median(latencies) * 1000, 3),
            "p95_ms": round(sorted(latencies)[int(runs * 0.95)] * 1000, 3),
            "min_ms": round(min(latencies) * 1000, 3),
            "max_ms": round(max(latencies) * 1000, 3),
        },
        "blame_attribution": {
            "runs": runs,
            "mean_ms": round(statistics.mean(blame_latencies) * 1000, 3),
        },
        "chain_info": {
            "total_agents": chain.total_agents,
            "max_depth": chain.max_depth,
            "grant_count": len(chain.collect_all_grants()),
        },
    }
