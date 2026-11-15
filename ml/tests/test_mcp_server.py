import asyncio
import json

import pytest

from buddy_ml.mcp_server import BuddyTools, build_server, build_tools, user_from_env
from buddy_ml.policy import SEED_USERS

from conftest import make_settings


@pytest.fixture(scope="module")
def tools(agent):
    return BuddyTools(SEED_USERS["usr_intern_01"], agent.retriever)


def test_user_from_env():
    ctx = {"user_id": "u", "role": "lead", "team_id": "team_auto_backend"}
    assert user_from_env({"BUDDY_USER_CONTEXT": json.dumps(ctx)}).role == "lead"
    assert user_from_env({"BUDDY_USER_ID": "usr_intern_02"}).team_id == "team_goods_search"
    assert user_from_env({}).user_id == "usr_intern_01"
    with pytest.raises(ValueError):
        user_from_env({"BUDDY_USER_ID": "hacker"})


def test_search_respects_acl(tools):
    hits = tools.search_knowledge("квоты лицензий кластера", k=50)
    assert len(hits) <= 10 and all(h["doc_id"] != "doc_03_lead_internal_policy" for h in hits)


def test_resources_and_policy(tools):
    res = {r["resource_id"]: r["allowed_levels"] for r in tools.list_resources()}
    assert res["db_fintech_prod_cluster"] == [] and res["space_goods_search_wiki"] == ["read"]
    assert tools.check_access_policy("space_goods_search_wiki", "write")["code"] == "RBAC_LEVEL_NOT_ALLOWED"


def test_propose_access_request_paths(tools):
    assert tools.propose_access_request("x", "read", "коротко")["status"] == "invalid"
    assert tools.propose_access_request("db_fintech_prod_cluster", "read", "Посмотреть данные платежей")["status"] == "denied"
    ok = tools.propose_access_request("space_auto_backend_wiki", "read", "Изучение документации команды")
    assert ok["status"] == "draft" and ok["operation_status"] == "awaiting_confirmation"
    assert ok["payload"]["approver_name"].startswith("Дмитрий")


def test_build_tools_from_settings():
    t = build_tools(make_settings(), user=SEED_USERS["usr_lead_01"])
    assert t.user.role == "lead" and t.search_knowledge("квоты лицензий", 1)


def test_mcp_server_registers_tools(tools):
    pytest.importorskip("mcp.server.mcpserver")
    server = build_server(tools)
    names = {t.name for t in asyncio.run(server.list_tools())}
    assert names == {"search_knowledge", "list_resources", "check_access_policy", "propose_access_request"}
