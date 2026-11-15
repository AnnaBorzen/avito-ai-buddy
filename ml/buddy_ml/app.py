"""HTTP API ML-сервиса (Flask). Вызывается backend'ом, не браузером.

Эндпоинты:
  GET  /healthz                 liveness
  GET  /readyz                  готовность: индекс построен, классификатор обучен, конфиг LLM
  GET  /v1/tools                схемы инструментов агента (function calling / MCP)
  POST /v1/agent/turn           один ход диалога → ответ, источники, слоты, tool_call
  POST /v1/knowledge/search     ACL-фильтрованный поиск (отладка, evals)
  POST /v1/nlu/parse            интент + слоты (отладка, evals)
"""

from __future__ import annotations

import logging
import time
import uuid

from flask import Flask, g, jsonify, request

from . import __version__
from .agent import Agent, RequestError, TurnRequest
from .config import Settings
from .knowledge import build_knowledge_base
from .nlu import IntentClassifier, extract_slots
from .policy import UserContext
from .retrieval import Retriever
from .tools import TOOLS

log = logging.getLogger("buddy_ml")


def build_agent(settings: Settings, encoder=None, llm_client=None) -> Agent:
    kb = build_knowledge_base(list(settings.kb_dirs), settings.playbook_dir)
    if encoder is None and "dense" in settings.retriever:  # pragma: no cover - тяжёлая зависимость
        from .retrieval import SentenceTransformerEncoder

        encoder = SentenceTransformerEncoder(settings.embedding_model)
    rankers = tuple(settings.retriever.split("+"))
    retriever = Retriever(kb, encoder=encoder, rankers=rankers)
    return Agent(kb, retriever, settings, IntentClassifier().fit(), llm_client=llm_client)


def _error(code: str, message: str, status: int):
    return jsonify({"error": {"code": code, "message": message, "request_id": g.get("request_id")}}), status


def create_app(settings: Settings | None = None, agent: Agent | None = None) -> Flask:
    settings = settings or Settings.from_env()
    agent = agent or build_agent(settings)
    app = Flask(__name__)
    app.json.ensure_ascii = False
    app.config["AGENT"] = agent

    @app.before_request
    def _start():
        g.request_id = request.headers.get("X-Request-Id") or f"ml-{uuid.uuid4().hex[:12]}"
        g.started = time.perf_counter()

    @app.after_request
    def _finish(resp):
        resp.headers["X-Request-Id"] = g.get("request_id", "")
        log.info(
            "request",
            extra={
                "method": request.method,
                "path": request.path,
                "status": resp.status_code,
                "duration_ms": round((time.perf_counter() - g.get("started", time.perf_counter())) * 1000, 1),
            },
        )
        return resp

    @app.errorhandler(404)
    def _not_found(_):
        return _error("NOT_FOUND", "route not found", 404)

    @app.errorhandler(405)
    def _method(_):
        return _error("METHOD_NOT_ALLOWED", "method not allowed", 405)

    @app.errorhandler(Exception)
    def _internal(exc):  # pragma: no cover - защитный обработчик
        log.exception("unhandled error")
        return _error("INTERNAL_ERROR", "internal error", 500)

    def _json_body() -> dict:
        body = request.get_json(silent=True)
        if not isinstance(body, dict):
            raise RequestError("body must be a JSON object")
        return body

    @app.get("/healthz")
    def healthz():
        return jsonify({"status": "ok"})

    @app.get("/readyz")
    def readyz():
        a: Agent = app.config["AGENT"]
        return jsonify(
            {
                "status": "ok",
                "version": __version__,
                "documents": len(a.kb.documents),
                "chunks": len(a.kb.chunks),
                "intents": a.classifier.classes,
                "config": a.settings.public(),
            }
        )

    @app.get("/v1/tools")
    def tools():
        return jsonify({"tools": TOOLS})

    @app.post("/v1/agent/turn")
    def agent_turn():
        try:
            req = TurnRequest.from_dict(_json_body())
        except RequestError as exc:
            return _error("VALIDATION_ERROR", str(exc), 400)
        req.request_id = req.request_id or g.request_id
        return jsonify(app.config["AGENT"].turn(req))

    def _user_from(body: dict) -> UserContext:
        raw = body.get("user")
        if not isinstance(raw, dict) or not all(k in raw for k in ("user_id", "role", "team_id")):
            raise RequestError("user.user_id, user.role and user.team_id are required")
        return UserContext.from_dict(raw)

    @app.post("/v1/knowledge/search")
    def knowledge_search():
        try:
            body = _json_body()
            user = _user_from(body)
            query = str(body.get("query") or "").strip()
            if not query:
                raise RequestError("query is required")
            k = max(1, min(int(body.get("k", 5)), 20))
        except (RequestError, ValueError) as exc:
            return _error("VALIDATION_ERROR", str(exc), 400)
        return jsonify({"hits": app.config["AGENT"].search(query, user, k)})

    @app.post("/v1/nlu/parse")
    def nlu_parse():
        try:
            body = _json_body()
            user = _user_from(body)
            text = str(body.get("message") or "").strip()
            if not text:
                raise RequestError("message is required")
        except RequestError as exc:
            return _error("VALIDATION_ERROR", str(exc), 400)
        a: Agent = app.config["AGENT"]
        pred = a.classifier.predict(text)
        return jsonify(
            {
                "intent": pred.intent,
                "confidence": round(pred.confidence, 4),
                "ranking": [[i, round(p, 4)] for i, p in pred.ranking],
                "slots": extract_slots(text, user, body.get("expected_slot")).to_dict(),
            }
        )

    return app


def main() -> None:  # pragma: no cover - точка входа для локального запуска
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
    settings = Settings.from_env()
    log.info("starting ml service: %s", settings.public())
    create_app(settings).run(host="0.0.0.0", port=settings.port)


if __name__ == "__main__":  # pragma: no cover
    main()
