"""DeVET backend multi-tenant sharding tests (P5).

Each stateful endpoint resolves its tenant from body tenant_id (default
"default"). Concurrent Bounty sessions no longer overwrite each other's
chains; /api/tenants + DELETE /api/tenants/{id} provide cleanup.
"""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))

from api_routes import (  # noqa: E402
    _TENANTS,
    build_scenario,
    delete_tenant,
    list_tenants,
    mirror_chain,
    tamper_chain,
    verify_chain,
)


def _reset():
    _TENANTS.clear()


def test_tenant_isolation():
    _reset()
    a = build_scenario({"tenant_id": "sess-a"})
    assert a["status"] == "built"
    assert a["tenant_id"] == "sess-a"

    # Tenant B has never built -> verify must 400.
    try:
        verify_chain({"tenant_id": "sess-b"})
        raise AssertionError("expected HTTPException for empty tenant")
    except Exception as e:
        assert getattr(e, "status_code", None) == 400

    # Tenant A verifies fine.
    v = verify_chain({"tenant_id": "sess-a"})
    assert v["tenant_id"] == "sess-a"
    assert v["result"]["authentic"] is True


def test_mirror_tamper_tenant_scoped():
    _reset()
    r = mirror_chain({
        "tenant_id": "sess-1",
        "host_name": "host-1",
        "agents": [{"name": "a1", "endpoint": "e1", "result_commitment": "c1"}],
    })
    assert r["status"] == "mirrored"
    assert r["tenant_id"] == "sess-1"

    v = verify_chain({"tenant_id": "sess-1"})
    assert v["result"]["authentic"] is True

    t = tamper_chain({"tenant_id": "sess-1", "delegate_index": 0, "commitment": "forged"})
    assert t["status"] == "tampered"
    assert t["result"]["authentic"] is False
    assert t["result"]["fault_type"] == "subagent_proof_invalid"

    # A different tenant is untouched (still empty).
    try:
        verify_chain({"tenant_id": "sess-2"})
        raise AssertionError("expected HTTPException for sess-2")
    except Exception as e:
        assert getattr(e, "status_code", None) == 400


def test_default_tenant_backward_compat():
    _reset()
    build_scenario({})
    rows = list_tenants()["tenants"]
    assert any(r["tenant_id"] == "default" and r["agents"] > 0 for r in rows)


def test_tenants_list_and_delete():
    _reset()
    mirror_chain({
        "tenant_id": "sess-x",
        "host_name": "h",
        "agents": [{"name": "a", "endpoint": "e", "result_commitment": "c"}],
    })
    rows = list_tenants()["tenants"]
    assert any(r["tenant_id"] == "sess-x" and r["agents"] == 2 for r in rows)

    d = delete_tenant("sess-x")
    assert d["existed"] is True
    assert all(r["tenant_id"] != "sess-x" for r in list_tenants()["tenants"])

    d2 = delete_tenant("sess-x")
    assert d2["existed"] is False
