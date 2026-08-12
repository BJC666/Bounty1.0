"""DeVET API Routes — wraps existing vet-repro modules as REST endpoints."""
import sys
import os
import time
import statistics
import json
import hashlib
import re

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
# Attack injection descriptions (what actually got tampered, for the demo UI)
# ---------------------------------------------------------------------------
ATTACK_INJECTIONS = {
    "A1_delegation_replacement": {
        "tamper_point": "delegation_tree[0].grant.delegatee_aid_hash",
        "tamper_desc": "把委托目标换成恶意 AID，且未重新封存 grant",
    },
    "A2_sub_result_forgery": {
        "tamper_point": "delegation_tree[0].agent_proof",
        "tamper_desc": "把执行证明换成错误 AID 的 CompositeProof",
    },
    "A3_depth_violation": {
        "tamper_point": "delegation_tree[0].grant.policy.max_delegation_depth + 追加子委托",
        "tamper_desc": "策略禁止继续委托（深度=0），却伪造子委托",
    },
    "A4_api_overrun": {
        "tamper_point": "delegation_tree[0].grant.policy.max_api_calls + 3 个子证明",
        "tamper_desc": "把调用上限降到 1，实际塞入 3 个子证明",
    },
    "A6_expired_grant": {
        "tamper_point": "delegation_tree[0].grant.policy.valid_until",
        "tamper_desc": "把过期时间改成 1970 年（已过期）并重新封存",
    },
    "A7_grant_tampering": {
        "tamper_point": "delegation_tree[0].grant.policy.max_api_calls",
        "tamper_desc": "修改策略字段但故意不重新封存（哈希失配）",
    },
    "A8_collusion": {
        "tamper_point": "delegation_tree[0].agent_proof",
        "tamper_desc": "Host A/B 共谋，把证明换成 EvilAgent 的证明",
    },
    "A10_replayed_grant": {
        "tamper_point": "delegation_tree[0].grant.policy.valid_until",
        "tamper_desc": "重放旧委托，过期时间改到 1970 年（已过期）",
    },
}

# ---------------------------------------------------------------------------
# CTF 挑战元数据：关卡分类 / 难度 / 提示 / 真实注入 payload / 蓝队旗
# ---------------------------------------------------------------------------
ATTACK_CHALLENGES = {
    "A1_delegation_replacement": {
        "category": "身份篡改",
        "difficulty": 1,
        "hint": "把委托目标换成恶意 AID，但不重新封存授权——哈希一比对就露馅。",
        "payload": 'grant.delegatee_aid_hash = "sha256:evil_<红队藏旗>"（未 seal）',
        "flag": "flag{devet_a1_blocked}",
    },
    "A2_sub_result_forgery": {
        "category": "结果伪造",
        "difficulty": 2,
        "hint": "伪造子 Agent 的执行证明，换成另一个 AID 的 CompositeProof——证明与受托方身份对不上。",
        "payload": "agent_proof = make_composite(aid_wrong)（端点藏旗）",
        "flag": "flag{devet_a2_blocked}",
    },
    "A3_depth_violation": {
        "category": "策略绕过",
        "difficulty": 2,
        "hint": "策略禁止再委托（深度=0），强行追加子委托。",
        "payload": "max_delegation_depth=0 + 子授权 notary_ref 藏旗",
        "flag": "flag{devet_a3_blocked}",
    },
    "A4_api_overrun": {
        "category": "策略绕过",
        "difficulty": 1,
        "hint": "把调用上限降到 1，实际塞 3 个子证明。",
        "payload": "max_api_calls=1 + 伪造 3 个子证明（请求串藏旗）",
        "flag": "flag{devet_a4_blocked}",
    },
    "A6_expired_grant": {
        "category": "时间攻击",
        "difficulty": 1,
        "hint": "翻出已过期的授权（1970 年）继续用。",
        "payload": "valid_until = 100.0（1970 年）+ 旧授权 notary_ref 藏旗",
        "flag": "flag{devet_a6_blocked}",
    },
    "A7_grant_tampering": {
        "category": "身份篡改",
        "difficulty": 3,
        "hint": "改策略字段但故意不重新封存——存储哈希与重算哈希失配。",
        "payload": "max_api_calls = 999 + notary_ref 藏旗（未 seal）",
        "flag": "flag{devet_a7_blocked}",
    },
    "A8_collusion": {
        "category": "跨宿主共谋",
        "difficulty": 3,
        "hint": "两个 Host 串通，把证明换成 EvilAgent 的证明。",
        "payload": "agent_proof = make_composite(aid_evil)（端点藏旗）",
        "flag": "flag{devet_a8_blocked}",
    },
    "A10_replayed_grant": {
        "category": "时间攻击",
        "difficulty": 1,
        "hint": "重放旧委托，过期时间拨到 1970 年（已过期）。",
        "payload": "valid_until = 1300.0 + 旧授权 notary_ref 藏旗（重放）",
        "flag": "flag{devet_a10_blocked}",
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


# ---------------------------------------------------------------------------
# 蓝队收旗：从被篡改的真实字段里正则识别 flag
# ---------------------------------------------------------------------------
_FLAG_RE = re.compile(r"flag\{[a-z0-9_]+\}")


def _extract_flags(chain, aid_registry=None):
    """Scan every tampered field for flag{...}; return [{"flag","source"}], dedup by source."""
    found = []

    def scan(value, source):
        if isinstance(value, str):
            for m in _FLAG_RE.finditer(value):
                found.append({"flag": m.group(0), "source": source})
        elif isinstance(value, dict):
            for k, v in value.items():
                scan(v, f"{source}.{k}")
        elif isinstance(value, (list, tuple)):
            for i, v in enumerate(value):
                scan(v, f"{source}[{i}]")

    if aid_registry:
        for _h, aid in aid_registry.items():
            scan(aid.core.endpoint, f"aid_registry[{aid.agent_name}].core.endpoint")
            for t in aid.tools:
                scan(t.endpoint, f"aid_registry[{aid.agent_name}].tools.{t.name}.endpoint")

    for idx, step in enumerate(chain.flatten()):
        grant = step.grant
        scan(grant.delegatee_aid_hash, f"delegation[{idx}].grant.delegatee_aid_hash")
        scan(grant.notary_attestation_ref, f"delegation[{idx}].grant.notary_attestation_ref")
        scan(grant.policy.to_dict(), f"delegation[{idx}].grant.policy")
        proof = step.agent_proof
        scan(proof.aid_hash, f"delegation[{idx}].agent_proof.aid_hash")
        for si, st in enumerate(proof.steps):
            scan(st.core.request, f"delegation[{idx}].agent_proof.steps[{si}].core.request")
            scan(st.core.response, f"delegation[{idx}].agent_proof.steps[{si}].core.response")
            for ti, tool in enumerate(st.tools):
                scan(tool.request, f"delegation[{idx}].agent_proof.steps[{si}].tools[{ti}].request")
                scan(tool.response, f"delegation[{idx}].agent_proof.steps[{si}].tools[{ti}].response")
        for pi, wp in enumerate(proof.all_sub_proofs()):
            scan(wp.server_name, f"delegation[{idx}].agent_proof.webproof[{pi}].server_name")
            scan(wp.attestation, f"delegation[{idx}].agent_proof.webproof[{pi}].attestation")

    seen = set()
    out = []
    for item in found:
        key = (item["flag"], item["source"])
        if key not in seen:
            seen.add(key)
            out.append(item)
    return out


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
        "trace": result.trace,
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
    t0 = time.perf_counter()
    _chain, _aid_registry = _scenario.build()
    build_elapsed_ms = round((time.perf_counter() - t0) * 1000, 3)

    return {
        "status": "built",
        "build_elapsed_ms": build_elapsed_ms,
        "chain": _serialize_chain(_chain),
        "aid_registry": [
            {
                "agent_name": aid.agent_name,
                "agent_hash": aid.agent_hash,
                "endpoint": aid.core.endpoint,
                "injection_algorithm_uid": aid.core.injection_algorithm_uid,
                "parsing_algorithm_uid": aid.core.parsing_algorithm_uid,
            }
            for aid in _aid_registry.values()
        ],
    }


@router.post("/chain/verify")
def verify_chain():
    """Verify the currently stored delegation chain."""
    global _scenario, _chain, _aid_registry

    if _chain is None or _scenario is None:
        raise HTTPException(status_code=400, detail="No chain built. Call /api/scenario/build first.")

    verifier = DelegationVerifier(root_aid=_scenario.strategy_aid)
    t0 = time.perf_counter()
    result = verifier.verify_chain(_chain, _aid_registry)
    verify_elapsed_ms = round((time.perf_counter() - t0) * 1000, 3)

    return {
        "status": "verified",
        "verify_elapsed_ms": verify_elapsed_ms,
        "result": _serialize_result(result),
    }


@router.get("/attacks")
def list_attacks():
    """Return available attack types with CTF challenge metadata."""
    merged = {}
    for k, info in ATTACKS.items():
        merged[k] = {
            **info,
            **ATTACK_INJECTIONS.get(k, {}),
            **ATTACK_CHALLENGES.get(k, {}),
        }
    return {"attacks": merged}


def _inject(chain, aid_registry, attack_type):
    """Apply the specified attack to a fresh chain (mutates in place)."""
    if attack_type == "A1_delegation_replacement":
        chain.delegation_tree[0].grant.delegatee_aid_hash = "sha256:evil_" + ATTACK_CHALLENGES[attack_type]["flag"]

    elif attack_type == "A2_sub_result_forgery":
        aid_wrong = _make_aid(name="WrongAgent", endpoint="api.evil.com/" + ATTACK_CHALLENGES[attack_type]["flag"])
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
            issuance_timestamp=time.time(), notary_attestation_ref="sha256:fake_" + ATTACK_CHALLENGES[attack_type]["flag"],
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
                ToolCallRecord(tool_name="API1", request="r1_" + ATTACK_CHALLENGES[attack_type]["flag"], response="r1", proof=wp2),
                ToolCallRecord(tool_name="API2", request="r2_" + ATTACK_CHALLENGES[attack_type]["flag"], response="r2", proof=wp3),
            ],
        )
        chain.delegation_tree[0].agent_proof = CompositeProof(
            aid_hash=delegatee_hash, steps=[step],
        )

    elif attack_type == "A6_expired_grant":
        chain.delegation_tree[0].grant.policy.valid_until = 100.0
        chain.delegation_tree[0].grant.notary_attestation_ref = "sha256:legacy_" + ATTACK_CHALLENGES[attack_type]["flag"]
        chain.delegation_tree[0].grant = chain.delegation_tree[0].grant.seal()

    elif attack_type == "A7_grant_tampering":
        chain.delegation_tree[0].grant.policy.max_api_calls = 999
        chain.delegation_tree[0].grant.notary_attestation_ref = "sha256:tampered_" + ATTACK_CHALLENGES[attack_type]["flag"]

    elif attack_type == "A8_collusion":
        aid_evil = _make_aid(name="EvilAgent", endpoint="api.evil.com/" + ATTACK_CHALLENGES[attack_type]["flag"])
        chain.delegation_tree[0].agent_proof = _make_composite(aid_evil)

    elif attack_type == "A10_replayed_grant":
        chain.delegation_tree[0].grant.policy.valid_until = 1300.0
        chain.delegation_tree[0].grant.notary_attestation_ref = "sha256:replayed_" + ATTACK_CHALLENGES[attack_type]["flag"]
        chain.delegation_tree[0].grant = chain.delegation_tree[0].grant.seal()

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
    _inject(chain, aid_registry, attack_type)

    # Verify the attacked chain    # Verify the attacked chain
    verifier = DelegationVerifier(root_aid=scenario.strategy_aid)
    t0 = time.perf_counter()
    result = verifier.verify_chain(chain, aid_registry)
    verify_elapsed_ms = round((time.perf_counter() - t0) * 1000, 3)

    detected = (not result.authentic)
    fault_match = (result.fault_type == expected_fault) if detected else False
    flags = _extract_flags(chain, aid_registry)

    return {
        "attack_type": attack_type,
        "attack_name": attack_info["name"],
        "attack_description": attack_info["description"],
        "expected_fault": expected_fault,
        "detected": detected,
        "fault_match": fault_match,
        "verify_elapsed_ms": verify_elapsed_ms,
        "injection": ATTACK_INJECTIONS.get(attack_type, {}),
        "challenge": ATTACK_CHALLENGES.get(attack_type, {}),
        "result": _serialize_result(result),
        "flags": flags,
        "flag_recovered": bool(flags),
    }


@router.post("/attack/simulate_all")
def simulate_all_attacks():
    """Run all 8 attacks sequentially; each is a fresh build + real injection + real verify."""
    rows = []
    for attack_type, attack_info in ATTACKS.items():
        scenario = TradingDAOScenario()
        chain, aid_registry = scenario.build()
        _inject(chain, aid_registry, attack_type)
        verifier = DelegationVerifier(root_aid=scenario.strategy_aid)
        t0 = time.perf_counter()
        result = verifier.verify_chain(chain, aid_registry)
        elapsed = round((time.perf_counter() - t0) * 1000, 3)
        detected = (not result.authentic)
        fault_match = (result.fault_type == attack_info["expected_fault"]) if detected else False
        fail_check = result.trace[-1] if result.trace else None
        flags = _extract_flags(chain, aid_registry)
        rows.append({
            "attack_type": attack_type,
            "attack_name": attack_info["name"],
            "attack_description": attack_info["description"],
            "expected_fault": attack_info["expected_fault"],
            "detected": detected,
            "fault_match": fault_match,
            "verify_elapsed_ms": elapsed,
            "fault_type": result.fault_type,
            "blame_path": result.blame_path,
            "fail_check": fail_check,
            "injection": ATTACK_INJECTIONS.get(attack_type, {}),
            "challenge": ATTACK_CHALLENGES.get(attack_type, {}),
            "trace": result.trace,
            "flags": flags,
            "flag_recovered": bool(flags),
        })
    return {
        "count": len(rows),
        "all_detected": all(r["detected"] and r["fault_match"] for r in rows),
        "rows": rows,
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
