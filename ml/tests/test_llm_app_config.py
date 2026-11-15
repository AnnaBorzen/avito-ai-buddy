import json

import pytest
import requests

from buddy_ml.app import create_app
from buddy_ml.config import Settings, parse_duration
from buddy_ml.llm import LLMError, OpenAICompatibleClient

# ---------------------------------------------------------------- LLM client


class FakeResp:
    def __init__(self, status=200, data=None, bad_json=False):
        self.status_code, self._data, self._bad = status, data, bad_json

    def json(self):
        if self._bad:
            raise ValueError("bad json")
        return self._data


class FakeSession:
    def __init__(self, resp=None, exc=None):
        self.resp, self.exc, self.sent = resp, exc, None

    def post(self, url, json=None, headers=None, timeout=None):
        self.sent = {"url": url, "json": json, "headers": headers, "timeout": timeout}
        if self.exc:
            raise self.exc
        return self.resp


def _client(session):
    return OpenAICompatibleClient("http://llm/v1/", "secret", "model-x", 5, session=session)


def test_llm_client_parses_content_and_tool_calls():
    data = {"model": "model-x", "usage": {"total_tokens": 10}, "choices": [{"message": {
        "content": None,
        "tool_calls": [
            {"function": {"name": "propose_access_request", "arguments": json.dumps({"resource_id": "r"})}},
            {"function": {"name": "broken", "arguments": "{not json"}},
            {"function": {"name": "dict_args", "arguments": {"a": 1}}},
        ]}}]}
    session = FakeSession(FakeResp(200, data))
    resp = _client(session).chat([{"role": "user", "content": "hi"}], tools=[{"type": "function"}])
    assert session.sent["url"] == "http://llm/v1/chat/completions"
    assert session.sent["headers"]["Authorization"] == "Bearer secret"
    assert session.sent["json"]["tool_choice"] == "auto" and session.sent["timeout"] == 5
    assert resp.content == "" and resp.usage == {"total_tokens": 10}
    assert resp.tool_calls[0].arguments == {"resource_id": "r"}
    assert resp.tool_calls[1].arguments is None
    assert resp.tool_calls[2].arguments == {"a": 1}


def test_llm_client_without_key_and_tools():
    session = FakeSession(FakeResp(200, {"choices": [{"message": {"content": "ok"}}]}))
    resp = OpenAICompatibleClient("http://llm", "", "m", session=session).chat([])
    assert "Authorization" not in session.sent["headers"] and "tools" not in session.sent["json"]
    assert resp.content == "ok" and resp.model == "m"


@pytest.mark.parametrize(
    "session,msg",
    [
        (FakeSession(exc=requests.Timeout()), "timeout"),
        (FakeSession(exc=requests.ConnectionError()), "transport"),
        (FakeSession(FakeResp(500, {})), "http 500"),
        (FakeSession(FakeResp(200, bad_json=True)), "malformed"),
        (FakeSession(FakeResp(200, {"choices": []})), "malformed"),
    ],
)
def test_llm_client_errors(session, msg):
    with pytest.raises(LLMError, match=msg):
        _client(session).chat([])


def test_llm_client_default_session():
    assert isinstance(OpenAICompatibleClient("http://x", "", "m").session, requests.Session)


# ---------------------------------------------------------------- config


def test_parse_duration():
    assert parse_duration("30s", 1) == 30
    assert parse_duration("1500ms", 1) == 1.5
    assert parse_duration("2m", 1) == 120
    assert parse_duration("12", 1) == 12
    assert parse_duration("", 7) == 7
    with pytest.raises(ValueError):
        parse_duration("soon", 1)


def test_settings_from_env_and_public():
    s = Settings.from_env({
        "ML_PORT": "9000", "LLM_ENABLED": "true", "LLM_BASE_URL": "http://llm", "LLM_MODEL": "m",
        "LLM_API_KEY": "secret", "LLM_TIMEOUT": "5s", "ML_RETRIEVER": "bm25+tfidf", "ML_EMBEDDING_MODEL": "e",
        "ML_KB_DIRS": "a", "ML_PLAYBOOK_DIR": "", "ML_INTENT_MIN_CONFIDENCE": "0.4", "ML_ANSWER_MIN_CONFIDENCE": "0.5",
    })
    assert s.port == 9000 and s.llm_enabled and s.llm_timeout == 5 and s.playbook_dir is None
    assert s.intent_min_confidence == 0.4 and s.answer_min_confidence == 0.5
    pub = s.public()
    assert pub["llm_api_key_set"] and "secret" not in json.dumps(pub) and "secret" not in repr(s)
    assert Settings.from_env({"ML_PLAYBOOK_DIR": "pb"}).playbook_dir.name == "pb"
    assert Settings.from_env({}).llm_enabled is False


def test_settings_validation():
    with pytest.raises(ValueError, match="LLM_BASE_URL"):
        Settings.from_env({"LLM_ENABLED": "1"})
    with pytest.raises(ValueError, match="ML_RETRIEVER"):
        Settings.from_env({"ML_RETRIEVER": "magic"})


# ---------------------------------------------------------------- HTTP API


@pytest.fixture(scope="module")
def client(agent):
    app = create_app(settings=agent.settings, agent=agent)
    return app.test_client()


def test_health_ready_tools(client):
    assert client.get("/healthz").get_json() == {"status": "ok"}
    ready = client.get("/readyz").get_json()
    assert ready["status"] == "ok" and ready["documents"] >= 6 and "request_access" in ready["intents"]
    assert client.get("/v1/tools").get_json()["tools"][0]["name"] == "propose_access_request"


def test_agent_turn_endpoint(client, intern1):
    resp = client.post("/v1/agent/turn", json={"user": intern1, "message": "Как запросить доступ?"},
                       headers={"X-Request-Id": "req-42"})
    assert resp.status_code == 200 and resp.headers["X-Request-Id"] == "req-42"
    body = resp.get_json()
    assert body["reply_kind"] == "answer" and body["sources"][0]["is_synthetic"] is True


def test_agent_turn_validation_errors(client):
    r = client.post("/v1/agent/turn", data="not json", content_type="application/json")
    assert r.status_code == 400 and r.get_json()["error"]["code"] == "VALIDATION_ERROR"
    r2 = client.post("/v1/agent/turn", json={"message": "hi"})
    assert r2.status_code == 400 and r2.get_json()["error"]["request_id"].startswith("ml-")


def test_search_and_nlu_endpoints(client, intern1):
    hits = client.post("/v1/knowledge/search", json={"user": intern1, "query": "квоты лицензий", "k": 50}).get_json()["hits"]
    assert all(h["doc_id"] != "doc_03_lead_internal_policy" for h in hits)
    assert client.post("/v1/knowledge/search", json={"user": intern1}).status_code == 400
    assert client.post("/v1/knowledge/search", json={"query": "x"}).status_code == 400
    assert client.post("/v1/knowledge/search", json={"user": intern1, "query": "x", "k": "a"}).status_code == 400
    parsed = client.post("/v1/nlu/parse", json={"user": intern1, "message": "нужен read к песочнице"}).get_json()
    assert parsed["slots"]["resource_id"] == "repo_auto_core_sandbox" and parsed["ranking"]
    assert client.post("/v1/nlu/parse", json={"user": intern1}).status_code == 400


def test_not_found_and_method(client):
    assert client.get("/nope").get_json()["error"]["code"] == "NOT_FOUND"
    assert client.get("/v1/agent/turn").status_code == 405


def test_create_app_builds_agent_from_settings():
    s = Settings()
    s.playbook_dir = None
    app = create_app(settings=s)
    assert app.test_client().get("/healthz").status_code == 200


def test_wsgi_entrypoint(monkeypatch):
    import importlib

    monkeypatch.setenv("ML_PLAYBOOK_DIR", "")
    monkeypatch.setenv("LLM_ENABLED", "false")
    wsgi = importlib.import_module("buddy_ml.wsgi")
    assert wsgi.app.test_client().get("/healthz").status_code == 200
