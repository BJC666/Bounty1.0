# veritrade/multi_agent_scenario.py
"""DeVET: Multi-agent trading scenario -- 3-level Trading DAO.

Simulates a hierarchical trading system:
  Strategy Agent (Root) -> Execution Agent (ETH) -> DEX API
                         -> Execution Agent (BTC) -> DEX API

Each agent produces a verifiable execution proof. The delegation
chain enables a third-party verifier to check the entire flow.
"""
import time
import hashlib
from dataclasses import dataclass, field
from typing import List, Dict, Optional

from vet_core.aid import AID, ComponentSpec, VerificationMeta
from vet_core.compositional import CompositeProof, ExecutionStep, CoreStepRecord, ToolCallRecord
from vet_core.execution_chain import ExecutionChain, ChainedStep
from vet_webproofs.prover import WebProof
from vet_core.delegation import (
    DelegationPolicy,
    DelegationGrant,
    DelegationExecutionStep,
    DelegationExecutionChain,
)


# ============================================================
# Mock helpers
# ============================================================

def _make_proof(server: str, commitment_seed: str) -> WebProof:
    """Create a deterministic mock WebProof for testing."""
    commitment = "sha256:" + hashlib.sha256(commitment_seed.encode()).hexdigest()
    return WebProof(
        server_name=server,
        attestation={
            "body": {
                "server_name": server,
                "transcript_commitment": commitment,
            },
            "signature": "0x" + hashlib.sha256(f"sig_{commitment_seed}".encode()).hexdigest(),
        },
        transcript_sent=f"GET / HTTP/1.1\r\nHost: {server}\r\n\r\n".encode(),
        transcript_recv=f"HTTP/1.1 200 OK\r\n\r\n{{'result': '{commitment_seed}'}}".encode(),
        commitment=commitment,
    )


def _make_composite(aid: AID, server: str, steps_data: List[dict]) -> CompositeProof:
    """Build a CompositeProof for an agent from step descriptions."""
    steps = []
    for i, sd in enumerate(steps_data):
        seed = f"{aid.agent_name}_step{i}"
        core_proof = _make_proof(server, seed)
        core_record = CoreStepRecord(
            request=sd['request'],
            response=sd['response'],
            proof=core_proof,
        )
        tools = []
        for j, (tool_name, treq, tresp) in enumerate(sd.get('tool_calls', [])):
            tool_seed = f"{aid.agent_name}_step{i}_tool{j}"
            tool_proof = _make_proof("api.dex.io", tool_seed)
            tools.append(ToolCallRecord(
                tool_name=tool_name,
                request=treq,
                response=tresp,
                proof=tool_proof,
            ))
        steps.append(ExecutionStep(core=core_record, tools=tools))
    return CompositeProof(aid_hash=aid.agent_hash, steps=steps)


def _make_aid(name: str, endpoint: str) -> AID:
    """Create a minimal AID for scenario agents."""
    return AID(
        agent_name=name,
        core=ComponentSpec(
            name="Core",
            endpoint=endpoint,
            injection_algorithm_uid=f"sha256:inject_{name}",
            parsing_algorithm_uid=f"sha256:parse_{name}",
        ),
    ).seal()


# ============================================================
# Scenario: 3-level Trading DAO
# ============================================================

@dataclass
class TradingDAOScenario:
    """Build a complete 3-level multi-agent trading delegation chain.

    Architecture:
      StrategyAgent (GPT-4o, api.openai.com)
        |-> ExecutionAgentETH (Claude Haiku, api.anthropic.com) -> DEX API
        |-> ExecutionAgentBTC (Claude Haiku, api.anthropic.com) -> DEX API
    """
    strategy_aid: AID = field(default_factory=lambda: _make_aid("StrategyAgent", "api.openai.com"))
    executor_eth_aid: AID = field(default_factory=lambda: _make_aid("ExecutionAgentETH", "api.anthropic.com"))
    executor_btc_aid: AID = field(default_factory=lambda: _make_aid("ExecutionAgentBTC", "api.anthropic.com"))

    # Delegation policies
    strategy_policy: DelegationPolicy = field(default_factory=lambda: DelegationPolicy(
        allowed_endpoints=["api.anthropic.com", "api.coingecko.com", "api.dex.io"],
        allowed_models=["claude-haiku-4.5", "claude-sonnet-4.5"],
        valid_until=time.time() + 86400,
        max_delegation_depth=1,
        max_api_calls=20,
    ))

    executor_policy: DelegationPolicy = field(default_factory=lambda: DelegationPolicy(
        allowed_endpoints=["api.dex.io"],
        allowed_models=["claude-haiku-4.5"],
        valid_until=time.time() + 86400,
        max_delegation_depth=0,
        max_api_calls=5,
    ))

    def build(self) -> tuple:
        """Build a complete, valid delegation execution chain.

        Returns:
            (chain, aid_registry) ready for verification
        """
        t0 = time.time()

        # -- Strategy Agent execution --
        strategy_chain = ExecutionChain(chain_id=self.strategy_aid.agent_hash)
        strategy_chain.append(ChainedStep(
            step_index=0, timestamp=t0, prev_hash=None,
            notary_commitments=["sha256:" + hashlib.sha256(b"strategy_analyze").hexdigest()],
        ))
        strategy_chain.append(ChainedStep(
            step_index=1, timestamp=t0 + 10, prev_hash=None,
            notary_commitments=["sha256:" + hashlib.sha256(b"strategy_decide").hexdigest()],
        ))

        strategy_composite = _make_composite(self.strategy_aid, "api.openai.com", [
            {
                "request": "Analyze current market conditions for ETH and BTC",
                "response": "ETH bullish (support $2500), BTC neutral. Recommend: "
                            "delegate ETH buy to ExecutionAgentETH, "
                            "delegate BTC hold to ExecutionAgentBTC.",
                "tool_calls": [
                    ("PriceFeedAPI", "GET /price?ids=ethereum,bitcoin",
                     '{"ethereum":{"usd":2500},"bitcoin":{"usd":50000}}'),
                ],
            },
            {
                "request": "Based on analysis, issue delegation grants for trade execution",
                "response": "Delegating ETH buy ($1500) to ExecutionAgentETH, "
                            "BTC hold to ExecutionAgentBTC.",
            },
        ])

        # -- Delegation grants --
        grant_eth = DelegationGrant(
            delegator_aid_hash=self.strategy_aid.agent_hash,
            delegatee_aid_hash=self.executor_eth_aid.agent_hash,
            policy=self.strategy_policy,
            issuance_timestamp=t0 + 10,
            notary_attestation_ref=strategy_chain.steps[-1].notary_commitments[0],
        ).seal()

        grant_btc = DelegationGrant(
            delegator_aid_hash=self.strategy_aid.agent_hash,
            delegatee_aid_hash=self.executor_btc_aid.agent_hash,
            policy=self.strategy_policy,
            issuance_timestamp=t0 + 10,
            notary_attestation_ref=strategy_chain.steps[-1].notary_commitments[0],
        ).seal()

        # -- Execution Agent ETH --
        eth_composite = _make_composite(self.executor_eth_aid, "api.anthropic.com", [
            {
                "request": "Execute ETH buy order: $1500 at market price",
                "response": "ETH buy order executed: 0.6 ETH at $2500/ETH",
                "tool_calls": [
                    ("DEXSwap", "POST /swap {pair: ETH/USDC, amount: 1500}",
                     '{"tx_hash":"0xabc123","amount_out":"0.6","asset":"ETH"}'),
                ],
            },
        ])

        # -- Execution Agent BTC --
        btc_composite = _make_composite(self.executor_btc_aid, "api.anthropic.com", [
            {
                "request": "Hold BTC position, no trade needed",
                "response": "BTC position held. No DEX interaction required.",
                "tool_calls": [],
            },
        ])

        # -- Assemble delegation tree --
        step_eth = DelegationExecutionStep(grant=grant_eth, agent_proof=eth_composite)
        step_btc = DelegationExecutionStep(grant=grant_btc, agent_proof=btc_composite)

        chain = DelegationExecutionChain(
            root_aid_hash=self.strategy_aid.agent_hash,
            root_chain=strategy_chain,
            delegation_tree=[step_eth, step_btc],
        )

        aid_registry = {
            self.strategy_aid.agent_hash: self.strategy_aid,
            self.executor_eth_aid.agent_hash: self.executor_eth_aid,
            self.executor_btc_aid.agent_hash: self.executor_btc_aid,
        }

        return chain, aid_registry
