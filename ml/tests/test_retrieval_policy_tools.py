import numpy as np
import pytest

from buddy_ml.knowledge import build_knowledge_base
from buddy_ml.policy import (
    CATALOG,
    SEED_USERS,
    Permissions,
    UserContext,
    check,
    default_permissions,
    level_ru,
)
from buddy_ml.retrieval import BM25, Retriever
from buddy_ml.tools import PROPOSE_ACCESS_REQUEST, openai_tools, validate_arguments

from conftest import make_settings


@pytest.fixture(scope="module")
def kb():
    s = make_settings()
    return build_knowledge_base(list(s.kb_dirs), None)


class FakeEncoder:
    """Детерминированный «эмбеддер» для проверки dense-ветки без torch."""

    name = "fake"

    def encode(self, texts, is_query=False):
        vecs = []
        for t in texts:
            t = t.lower()
            v = np.array([t.count("соглас"), t.count("прод") + t.count("промышлен"), t.count("чек"), 1.0])
            vecs.append(v / np.linalg.norm(v))
        return np.array(vecs)


def test_acl_is_applied_before_ranking(kb):
    r = Retriever(kb)
    hits = r.search("внутренний SLA квоты лицензий кластера Auto", "intern", "team_auto_backend", k=10).hits
    assert all(h.chunk.doc.doc_id != "doc_03_lead_internal_policy" for h in hits)
    lead_hits = r.search("внутренний SLA квоты лицензий кластера Auto", "lead", "team_auto_backend", k=3).hits
    assert lead_hits[0].chunk.doc.doc_id == "doc_03_lead_internal_policy"
    mentor_only = r.search("напутственное слово стажёру на экваторе", "intern", "team_auto_backend", k=10).hits
    assert all(h.chunk.doc.doc_id != "doc_06_mentor_checklist" for h in mentor_only)


def test_search_ranking_coverage_and_sources(kb):
    r = Retriever(kb, rankers=("bm25", "tfidf"))
    assert r.mode == "bm25+tfidf"
    res = r.search("сколько ждать согласования заявки", "intern", "team_auto_backend", k=3)
    assert res.top.chunk.doc.doc_id == "doc_01_onboarding_access"
    assert 0 < res.confidence <= 1
    src = res.top.to_source(excerpt_chars=40)
    assert src["is_synthetic"] and src["source_label"] == "Тестовая инструкция" and src["excerpt"].endswith("…")
    only = r.search("согласование", "intern", "team_auto_backend", doc_ids={"doc_02_security_restrictions"})
    assert {h.chunk.doc.doc_id for h in only.hits} <= {"doc_02_security_restrictions"}


def test_search_edge_cases(kb):
    r = Retriever(kb)
    assert r.search("   ", "intern", "team_auto_backend").hits == []
    assert r.search("доступ", "guest", "team_x").hits == []  # нет доступных документов
    res = Retriever(kb, rankers=("bm25",)).search("абракадабра zzzz", "intern", "team_auto_backend")
    assert res.hits == [] and res.confidence == 0.0 and res.top is None
    # символьные n-граммы находят «что-то», но покрытие терминов нулевое → уверенность 0
    assert r.search("абракадабра zzzz", "intern", "team_auto_backend").confidence == 0.0
    assert r.coverage(set(), 0) == 0.0


def test_oov_ratio(kb):
    r = Retriever(kb)
    assert r.oov_ratio("согласование заявки", "intern", "team_auto_backend") == 0.0
    assert r.oov_ratio("курс доллара биткоин", "intern", "team_auto_backend") > 0.6
    assert r.oov_ratio("и в на", "intern", "team_auto_backend") == 0.0
    # термины закрытого документа для стажёра считаются неизвестными (нет побочного канала)
    assert r.oov_ratio("лицензий квартал sla", "intern", "team_auto_backend") > r.oov_ratio(
        "лицензий квартал sla", "lead", "team_auto_backend"
    )


def test_dense_ranker_with_fake_encoder(kb):
    r = Retriever(kb, encoder=FakeEncoder(), rankers=("bm25", "dense"))
    assert r.mode == "bm25+dense"
    res = r.search("промышленная база", "intern", "team_auto_backend", k=3)
    assert res.hits and any(h.dense > 0 for h in res.hits)
    # без энкодера dense-ранкер молча отключается
    assert Retriever(kb, encoder=None, rankers=("bm25", "dense")).mode == "bm25"


def test_bm25_empty_corpus():
    bm = BM25([])
    assert bm.scores(["x"], np.array([], dtype=int)).size == 0


# ---------------------------------------------------------------- policy


def test_default_permissions_matrix_from_spec():
    p1 = default_permissions(SEED_USERS["usr_intern_01"])
    assert p1.levels("space_auto_backend_wiki") == ("read", "write")
    assert p1.levels("repo_auto_core_sandbox") == ("read", "write")
    assert p1.levels("space_goods_search_wiki") == ("read",)
    assert p1.levels("db_fintech_prod_cluster") == ()
    p2 = default_permissions(SEED_USERS["usr_intern_02"])
    assert p2.levels("space_goods_search_wiki") == ("read",)
    assert p2.levels("space_auto_backend_wiki") == ("read",)
    assert p2.levels("repo_auto_core_sandbox") == ()
    lead = default_permissions(SEED_USERS["usr_lead_01"])
    assert lead.levels("repo_auto_core_sandbox") and lead.levels("db_fintech_prod_cluster") == ()
    guest = default_permissions(UserContext("u", "x", "guest", "t"))
    assert all(not v for v in guest.allowed.values())


def test_permissions_from_backend_override_seed():
    user = SEED_USERS["usr_intern_01"]
    p = Permissions.from_request([{"resource_id": "space_auto_backend_wiki", "allowed_levels": ["read"]}], user)
    assert p.levels("space_auto_backend_wiki") == ("read",)
    assert p.levels("repo_auto_core_sandbox") == ()
    assert Permissions.from_request(None, user).levels("repo_auto_core_sandbox") == ("read", "write")


def test_policy_check_codes():
    u = SEED_USERS["usr_intern_01"]
    p = default_permissions(u)
    assert check(u, p, "space_auto_backend_wiki", "write").allowed
    assert check(u, p, "space_auto_backend_wiki", None).allowed_levels == ("read", "write")
    assert check(u, p, "space_goods_search_wiki", "write").code == "RBAC_LEVEL_NOT_ALLOWED"
    assert check(u, p, "db_fintech_prod_cluster", "read").code == "RBAC_ACCESS_DENIED"
    assert check(u, p, "space_auto_backend_wiki", "admin").code == "LEVEL_UNSUPPORTED"
    d = check(u, p, "unknown_res", "read")
    assert d.code == "RESOURCE_UNKNOWN" and d.to_dict()["allowed"] is False
    assert level_ru("read").startswith("чтение") and level_ru("x") == "x"


def test_user_context_from_dict_fills_team_name():
    u = UserContext.from_dict({"user_id": "u", "role": "intern", "team_id": "team_goods_search"})
    assert u.team_name == "Поиск Товаров"


# ---------------------------------------------------------------- tools


def test_tool_schema_matches_spec():
    params = PROPOSE_ACCESS_REQUEST["parameters"]
    assert set(params["required"]) == {"resource_id", "access_level", "justification"}
    assert params["additionalProperties"] is False
    assert "user_id" not in params["properties"] and "approver_id" not in params["properties"]
    assert params["properties"]["resource_id"]["enum"] == list(CATALOG)
    assert openai_tools()[0]["type"] == "function"


@pytest.mark.parametrize(
    "args,error",
    [
        ("str", "object"),
        ({}, "missing required field: resource_id"),
        ({"resource_id": "x", "access_level": "read", "justification": "достаточно длинно"}, "catalog"),
        ({"resource_id": "space_auto_backend_wiki", "access_level": "admin", "justification": "достаточно длинно"}, "read or write"),
        ({"resource_id": "space_auto_backend_wiki", "access_level": "read", "justification": "коротко"}, "10..300"),
        ({"resource_id": "space_auto_backend_wiki", "access_level": "read", "justification": "достаточно длинно", "user_id": "x"}, "unexpected"),
    ],
)
def test_validate_arguments_errors(args, error):
    assert any(error in e for e in validate_arguments(args))


def test_validate_arguments_ok():
    assert validate_arguments(
        {"resource_id": "space_auto_backend_wiki", "access_level": "read", "justification": "Изучение документации"}
    ) == []
