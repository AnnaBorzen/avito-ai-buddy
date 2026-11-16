"""Агент AI-buddy: маршрутизация, ответы по документам, сбор параметров и черновик заявки.

Два режима, одинаковый контракт ответа:
  * local — детерминированный: NLU + RAG + шаблоны с извлечёнными фрагментами документов.
    Работает без ключей и сети, < 50 мс; это fallback для LLM-режима.
  * llm — LLM с function calling поверх того же RAG. Решения LLM проходят валидацию:
    схема инструмента, «заземлённость» параметров в словах пользователя, RBAC, проверки
    ответа (утечки, ложный успех). Любая ошибка LLM → ответ локального режима.

Сервис stateless: состояние (черновик, операция, история) хранит backend и передаёт в запросе.
"""

from __future__ import annotations

import re
import time
from dataclasses import dataclass, field
from string import Template

from . import SCENARIO_ID, SCENARIO_VERSION, __version__
from .config import PACKAGE_DIR, Settings
from .guard import (
    GuardReport,
    detect_injection,
    enforce_status_truth,
    leaks_forbidden,
    sanitize_document,
    vocabulary,
)
from .knowledge import Chunk, KnowledgeBase
from .llm import LLMError, OpenAICompatibleClient
from .nlu import (
    IntentClassifier,
    Slots,
    access_target,
    extract_level,
    extract_resource,
    extract_slots,
    fuzzy_fix,
    is_question,
)
from .policy import CATALOG, Permissions, UserContext, check, level_ru
from .retrieval import Hit, Retriever
from .text import content_tokens, normalize
from .tools import PROPOSE_ACCESS_REQUEST, openai_tools, validate_arguments

SYNTHETIC_DOCS = {"doc_01_onboarding_access", "doc_02_security_restrictions", "doc_03_lead_internal_policy"}
CHECKLIST_DOCS = {"doc_04_intern_checklist", "doc_05_backend_checklist", "doc_06_mentor_checklist"}
LOCKED_OPERATION_STATUSES = {"submitting", "unknown"}
QA_INTENTS = {"ask_procedure", "ask_levels", "ask_approval", "ask_restrictions", "ask_plan", "ask_tools"}

_PERIODS = [
    (re.compile(r"втор\w* недел|2[- ]?(ой|я)? недел"), "2 неделя"),
    (re.compile(r"перв\w* месяц|1[- ]?(ый)? месяц|месяц"), "1 месяц"),
    (re.compile(r"втор\w* месяц|2[- ]?(ой)? месяц"), "2 месяц"),
    (re.compile(r"трет\w* месяц|3[- ]?(ий)? месяц"), "3 месяц"),
    (re.compile(r"экватор|после тр[её]х|после 3|выпускн|книг|почитать"), "После 3-х месяцев"),
    (re.compile(r"перв\w* недел|1[- ]?(ая)? недел|первые дни|начать|начал"), "1 неделя"),
]


# ---------------------------------------------------------------- request / response


class RequestError(ValueError):
    """Невалидный запрос к агенту (→ HTTP 400)."""


@dataclass
class TurnRequest:
    user: UserContext
    permissions: Permissions
    message: str
    draft: Slots = field(default_factory=Slots)
    operation: dict | None = None
    history: list[dict] = field(default_factory=list)
    expected_slot: str | None = None
    request_id: str | None = None
    task_instance_id: str | None = None
    route_confidence: float = 1.0  # заполняется маршрутизатором

    @classmethod
    def from_dict(cls, data: dict) -> "TurnRequest":
        if not isinstance(data, dict):
            raise RequestError("body must be a JSON object")
        message = data.get("message")
        if not isinstance(message, str) or not message.strip():
            raise RequestError("message is required")
        if len(message) > 2000:
            raise RequestError("message is too long (max 2000 chars)")
        user_raw = data.get("user")
        if not isinstance(user_raw, dict) or not all(k in user_raw for k in ("user_id", "role", "team_id")):
            raise RequestError("user.user_id, user.role and user.team_id are required")
        if user_raw["role"] not in ("intern", "mentor", "lead"):
            raise RequestError("user.role must be intern|mentor|lead")
        user = UserContext.from_dict(user_raw)
        history = data.get("history") or []
        if not isinstance(history, list):
            raise RequestError("history must be a list")
        history = [
            {"role": h.get("role"), "content": str(h.get("content", ""))[:2000]}
            for h in history[-12:]
            if isinstance(h, dict) and h.get("role") in ("user", "assistant")
        ]
        expected = data.get("expected_slot")
        if expected not in (None, "resource_id", "access_level", "justification"):
            raise RequestError("expected_slot is invalid")
        operation = data.get("operation")
        if operation is not None and not isinstance(operation, dict):
            raise RequestError("operation must be an object")
        return cls(
            user=user,
            permissions=Permissions.from_request(data.get("permissions"), user),
            message=message.strip(),
            draft=Slots.from_dict(data.get("draft")),
            operation=operation,
            history=history,
            expected_slot=expected,
            request_id=data.get("request_id"),
            task_instance_id=data.get("task_instance_id"),
        )


@dataclass
class TurnResult:
    reply: str
    reply_kind: str
    intent: str
    intent_confidence: float = 0.0
    sources: list[dict] = field(default_factory=list)
    slots: Slots = field(default_factory=Slots)
    missing_slots: list[str] = field(default_factory=list)
    expected_slot: str | None = None
    tool_call: dict | None = None
    policy: dict | None = None
    handoff_suggested: bool = False
    guard: GuardReport = field(default_factory=GuardReport)
    meta: dict = field(default_factory=dict)

    def to_dict(self, user: UserContext) -> dict:
        source_ids = sorted({s["doc_id"] for s in self.sources})
        event = "action_proposed" if self.tool_call else "answer_delivered"
        return {
            "reply": self.reply,
            "reply_kind": self.reply_kind,
            "intent": self.intent,
            "intent_confidence": round(self.intent_confidence, 4),
            "sources": self.sources,
            "slots": self.slots.to_dict(),
            "missing_slots": self.missing_slots,
            "expected_slot": self.expected_slot,
            "tool_call": self.tool_call,
            "policy": self.policy,
            "handoff_suggested": self.handoff_suggested,
            "handoff_target": user.mentor_handle or None,
            "guard": self.guard.to_dict(),
            "meta": self.meta,
            # Подсказка backend'у для tracking plan: без текста диалога и закрытого содержимого.
            "analytics": {
                "event": event,
                "properties": {
                    "intent": self.intent,
                    "reply_kind": self.reply_kind,
                    "source_doc_ids": source_ids,
                    "handoff_suggested": self.handoff_suggested,
                    "injection_detected": self.guard.injection_detected,
                    "mode": self.meta.get("mode"),
                    "scenario_id": SCENARIO_ID,
                    "scenario_version": SCENARIO_VERSION,
                },
            },
        }


# ---------------------------------------------------------------- agent


def _first_name(user: UserContext) -> str:
    return user.name.split(" ")[0] if user.name else ""


def _clip(text: str, limit: int = 700) -> str:
    text = re.sub(r"`", "", text).strip()
    if len(text) <= limit:
        return text
    return text[:limit].rsplit(" ", 1)[0] + "…"


class Agent:
    def __init__(self, kb: KnowledgeBase, retriever: Retriever, settings: Settings | None = None,
                 classifier: IntentClassifier | None = None, llm_client=None):
        self.kb = kb
        self.retriever = retriever
        self.settings = settings or Settings()
        self.classifier = classifier or IntentClassifier().fit()
        self.llm = llm_client
        if self.llm is None and self.settings.llm_enabled:
            self.llm = OpenAICompatibleClient(
                self.settings.llm_base_url, self.settings.llm_api_key,
                self.settings.llm_model, self.settings.llm_timeout,
            )
        self._vocab_cache: dict[tuple[str, str], set[str]] = {}
        self.system_template = Template((PACKAGE_DIR / "prompts" / "system_ru.md").read_text(encoding="utf-8"))

    # ------------------------------------------------------------ public API

    def turn(self, req: TurnRequest) -> dict:
        started = time.perf_counter()
        result = self._local(req)
        mode = "local"
        llm_error = None
        if self.llm is not None and self._llm_eligible(result):
            try:
                result = self._with_llm(req, result)
                mode = "llm"
            except LLMError as exc:
                mode, llm_error = "local_fallback", str(exc)
        result.meta = {
            "mode": mode,
            "model": self.settings.llm_model if mode == "llm" else "local-nlu",
            "retriever": self.retriever.mode,
            "latency_ms": round((time.perf_counter() - started) * 1000, 1),
            "scenario_id": SCENARIO_ID,
            "scenario_version": SCENARIO_VERSION,
            "service_version": __version__,
        }
        if llm_error:
            result.meta["llm_error"] = llm_error
        return result.to_dict(req.user)

    def leaks(self, reply: str, user: UserContext) -> list[str]:
        """Doc_id закрытых для пользователя документов, содержимое которых попало в ответ."""
        key = (user.role, user.team_id)
        if key not in self._vocab_cache:
            self._vocab_cache[key] = vocabulary(self.kb.allowed_chunks(*key))
        return leaks_forbidden(reply, self.kb.forbidden_chunks(*key), allowed_vocab=self._vocab_cache[key])

    def search(self, query: str, user: UserContext, k: int = 5) -> list[dict]:
        return [h.to_source() for h in self.retriever.search(query, user.role, user.team_id, k=k).hits]

    # ------------------------------------------------------------ routing

    def _expected_slot(self, req: TurnRequest) -> str | None:
        if req.expected_slot:
            return req.expected_slot
        if req.draft.any():
            missing = req.draft.missing()
            return missing[0] if missing else None
        return None

    def route(self, req: TurnRequest) -> tuple[str, float, Slots, GuardReport]:
        msg = req.message
        norm = fuzzy_fix(normalize(msg))
        guard = detect_injection(msg)
        expected = self._expected_slot(req)
        new_slots = extract_slots(msg, req.user, expected)
        pred = self.classifier.predict(msg)
        question = is_question(msg)
        has_request_slots = bool(new_slots.resource_id or new_slots.access_level)

        # Явная атака (подмена system, вывод промпта, «забудь правила», чужие данные) —
        # отказ, даже если в тексте есть похожие на параметры слова («администратор» → admin).
        hard_flags = {"prompt_exfiltration", "system_spoof", "override_instructions", "foreign_data"}
        if set(guard.flags) & hard_flags:
            return "injection", 1.0, Slots(), guard
        if guard.injection_detected and has_request_slots:
            return "request_access", max(pred.confidence, 0.5), new_slots, guard
        if guard.injection_detected:
            return "injection", pred.confidence, new_slots, guard
        if pred.intent == "injection" and pred.confidence >= 0.5:
            # Только классификатор, без явных паттернов атаки: вопрос может быть легитимным для
            # роли с доступом к закрытому документу (lead → квоты). Решает ACL в поиске.
            return "injection_soft", pred.confidence, new_slots, guard

        target = access_target(norm)
        if target and not extract_resource(norm, req.user) and pred.intent in (
            "ask_procedure", "request_access", "out_of_scope", "ask_tools",
        ):
            return "out_of_catalog", pred.confidence, new_slots, guard

        draft_in_progress = req.draft.any() and bool(req.draft.missing())
        short_reply = len(norm.split()) <= 3 and pred.intent not in ("greeting", "handoff", "check_status")
        answers_expected = (
            draft_in_progress
            and not question
            and (new_slots.any() or (expected == "justification" and new_slots.justification) or short_reply)
        )
        if pred.intent == "request_access" and not (question and pred.confidence < 0.5):
            return "request_access", pred.confidence, new_slots, guard
        if answers_expected or (has_request_slots and not question and pred.intent not in ("check_status", "handoff")):
            return "request_access", max(pred.confidence, 0.5), new_slots, guard
        if re.match(r"^(привет|здравств|добр\w+ (день|утро|вечер)|хай|салют)", norm) and not question:
            return "greeting", max(pred.confidence, 0.5), new_slots, guard
        op_status = (req.operation or {}).get("operation_status")
        if pred.intent == "ask_approval" and op_status in ("failed", "unknown", "submitting"):
            return "check_status", pred.confidence, new_slots, guard  # «заявка ушла?» при сбое — это вопрос о статусе
        if (req.operation and pred.confidence < 0.5 and question
                and re.search(r"заявк|доступ|статус|одобр|соглас", norm)):
            return "check_status", pred.confidence, new_slots, guard  # есть операция — неуверенный вопрос о ней
        if (question and new_slots.resource_id and new_slots.access_level and pred.confidence < 0.5
                and pred.intent in ("ask_procedure", "ask_levels", "out_of_scope", "request_access")):
            # «Как мне получить write к вики соседней команды?» — отвечаем проверкой политики.
            return "request_access", pred.confidence, new_slots, guard
        if question and new_slots.resource_id and pred.confidence < self.settings.intent_min_confidence:
            return "ask_restrictions", pred.confidence, new_slots, guard  # вопрос о конкретном ресурсе
        if (pred.intent in QA_INTENTS - {"ask_plan"} and pred.confidence < 0.6 and not new_slots.resource_id
                and self.retriever.oov_ratio(req.message, req.user.role, req.user.team_id) > 0.6):
            # Существенная часть запроса — термины, которых нет в доступных материалах («курс доллара»).
            return "out_of_scope", pred.confidence, new_slots, guard
        if pred.confidence < self.settings.intent_min_confidence:
            return "uncertain", pred.confidence, new_slots, guard
        return pred.intent, pred.confidence, new_slots, guard

    # ------------------------------------------------------------ local mode

    def _local(self, req: TurnRequest) -> TurnResult:
        intent, conf, new_slots, guard = self.route(req)
        req.route_confidence = conf
        handler = {
            "greeting": self._greeting,
            "ask_procedure": self._ask_procedure,
            "ask_levels": self._ask_levels,
            "ask_approval": self._ask_approval,
            "ask_restrictions": self._ask_restrictions,
            "ask_plan": self._ask_plan,
            "ask_tools": self._ask_open,
            "uncertain": self._ask_open,
            "request_access": self._request_access,
            "check_status": self._check_status,
            "handoff": self._handoff,
            "out_of_scope": self._out_of_scope,
            "out_of_catalog": self._out_of_catalog,
            "injection": self._injection,
            "injection_soft": self._injection_soft,
        }[intent]
        result = handler(req, new_slots, guard)
        if intent == "injection_soft":
            intent = "injection" if result.reply_kind == "denied" else "ask_tools"
        result.intent = intent
        result.intent_confidence = conf
        result.guard = guard
        if result.slots is None or not result.slots.any():
            result.slots = req.draft
        return result

    def _src(self, hits: list[Hit]) -> list[dict]:
        seen, out = set(), []
        for h in hits:
            key = (h.chunk.doc.doc_id, h.chunk.section)
            if key not in seen:
                seen.add(key)
                out.append(h.to_source())
        return out

    def _section_hit(self, user: UserContext, doc_id: str, section_prefix: str) -> Hit | None:
        chunk = self.kb.section(doc_id, section_prefix)
        if chunk is None or not chunk.doc.allows(user.role, user.team_id):
            return None
        return Hit(chunk=chunk, score=1.0)

    def _search(self, req: TurnRequest, query: str, docs: set[str] | None, k: int = 3):
        return self.retriever.search(query, req.user.role, req.user.team_id, k=k, doc_ids=docs)

    def _greeting(self, req, slots, guard) -> TurnResult:
        if re.search(r"спасиб|благодар", normalize(req.message)):
            text = "Пожалуйста! Если появятся вопросы по доступам или следующим шагам адаптации — пишите."
        else:
            text = (
                f"Привет{', ' + _first_name(req.user) if req.user.name else ''}! Я AI-buddy команды {req.user.team_name}. "
                "Помогу разобраться со следующими шагами адаптации и оформить доступ к пространству команды: "
                "подскажу порядок, соберу параметры заявки и покажу её статус. "
                "Например, спросите «Как запросить доступ?» или напишите, какой доступ нужен."
            )
        return TurnResult(text, "greeting", "greeting")

    def _ask_procedure(self, req, slots, guard) -> TurnResult:
        res = self._search(req, req.message + " порядок запроса доступа параметры заявки", {"doc_01_onboarding_access"}, k=2)
        params = self._section_hit(req.user, "doc_01_onboarding_access", "Какие параметры")
        why = self._section_hit(req.user, "doc_01_onboarding_access", "Зачем нужен")
        hits = [h for h in (why, params) if h] or res.hits
        if not hits:
            return self._no_data(req, slots, guard)
        own = [r for r in CATALOG.values() if r.owner_team == req.user.team_id and req.permissions.levels(r.resource_id)]
        own_txt = ", ".join(f"{r.title} (`{r.resource_id}`)" for r in own) or "ресурсы своей команды"
        text = (
            f"Для команды {req.user.team_name} на первой неделе нужен доступ к: {own_txt}. "
            "По тестовой инструкции заявка содержит три параметра: ресурс, уровень прав "
            "(read — чтение материалов, write — заметки и отчёты по онбордингу) и короткое обоснование. "
            "Напишите, например: «Нужен доступ на чтение к базе знаний команды для изучения документации» — "
            "я подготовлю карточку, вы проверите её и подтвердите отправку."
        )
        if req.user.approver_name:
            text += f" Заявку согласует {req.user.approver_name}."
        return TurnResult(text, "answer", "ask_procedure", sources=self._src(hits))

    def _ask_levels(self, req, slots, guard) -> TurnResult:
        hit = self._section_hit(req.user, "doc_01_onboarding_access", "Какие параметры")
        if not hit:
            return self._no_data(req, slots, guard)
        text = (
            "По тестовой инструкции доступны два уровня: `read` — только чтение материалов, "
            "`write` — создание заметок и отчётов по онбордингу. Выбирайте минимально достаточный уровень для задачи."
        )
        sources = [hit]
        if slots.resource_id:
            decision = check(req.user, req.permissions, slots.resource_id, None)
            title = CATALOG[slots.resource_id].title
            if decision.allowed:
                text += f" Для ресурса «{title}» вам доступно: " + ", ".join(level_ru(lv) for lv in decision.allowed_levels) + "."
            else:
                text += f" Ресурс «{title}» вам недоступен для запроса."
                rules = self._section_hit(req.user, "doc_02_security_restrictions", "Матрица")
                sources += [rules] if rules else []
        return TurnResult(text, "answer", "ask_levels", sources=self._src(sources))

    def _ask_approval(self, req, slots, guard) -> TurnResult:
        hit = self._section_hit(req.user, "doc_01_onboarding_access", "Как происходит согласование")
        if not hit:
            return self._no_data(req, slots, guard)
        approver = req.user.approver_name or "руководитель команды"
        text = (
            f"После подтверждения карточки создаётся заявка со статусом `pending` — это ожидание согласования, "
            f"а не выданный доступ. Её рассматривает руководитель команды — в вашем случае {approver}; "
            "стандартный срок по тестовой инструкции — до 4 рабочих часов. Пока ждёте, можно изучить открытый "
            "AvitoTech Playbook или подготовить вопросы к первой встрече 1:1 с наставником."
        )
        ticket = (req.operation or {}).get("ticket") or {}
        if ticket.get("ticket_id"):
            text += f" Ваша заявка {ticket['ticket_id']} сейчас в статусе `{ticket.get('external_status', 'pending')}`."
        return TurnResult(text, "answer", "ask_approval", sources=self._src([hit]))

    def _ask_restrictions(self, req, slots, guard) -> TurnResult:
        res = self._search(req, req.message, {"doc_02_security_restrictions"}, k=2)
        matrix = self._section_hit(req.user, "doc_02_security_restrictions", "Матрица")
        blocked = self._section_hit(req.user, "doc_02_security_restrictions", "Действия при блокировке")
        if not (matrix or res.hits):
            return self._no_data(req, slots, guard)
        norm = normalize(req.message)
        wants_data = bool(re.search(r"дамп|выгрузк|данн\w+ из|прод|промышлен|боев|отказал|нехватк|заблокир", norm))
        parts = [
            "По тестовой инструкции стажёр может запрашивать `read` и `write` только к учебным репозиториям "
            "и пространству базы знаний своей команды; к базам знаний смежных команд — только `read`. "
            "Доступ стажёров к промышленным (Production) базам данных и финансовым контурам закрыт."
        ]
        sources = [h for h in (matrix,) if h]
        if wants_data and blocked:
            parts.append(
                f"Если для задачи нужны данные из промышленного контура, доступ не запрашивается — "
                f"попросите наставника {req.user.mentor_handle or ''} подготовить обезличенный дамп.".replace("  ", " ")
            )
            sources.append(blocked)
        if slots.resource_id:
            d = check(req.user, req.permissions, slots.resource_id, slots.access_level if slots.access_level in ("read", "write") else None)
            title = CATALOG[slots.resource_id].title
            parts.append(
                f"Для вас: «{title}» — " + ("можно запросить (" + ", ".join(d.allowed_levels) + ")." if d.allowed else "запрос недоступен.")
            )
        return TurnResult(" ".join(parts), "answer", "ask_restrictions", sources=self._src(sources or res.hits),
                          handoff_suggested=wants_data)

    def _ask_plan(self, req, slots, guard) -> TurnResult:
        norm = normalize(req.message)
        period = next((p for rx, p in _PERIODS if rx.search(norm)), None)
        explicit_period = period is not None
        period = period or "1 неделя"
        allowed = [c for c in self.kb.allowed_chunks(req.user.role, req.user.team_id) if c.doc.doc_id in CHECKLIST_DOCS]
        items = [c for c in allowed if c.section.lower().startswith(period.lower())]
        if req.user.role in ("mentor", "lead"):  # для наставника его собственные пункты — первыми
            items.sort(key=lambda c: c.doc.doc_id != "doc_06_mentor_checklist")
        if not items:
            return self._no_data(req, slots, guard)
        titles = []
        for c in items:
            title = c.section.split(":", 1)[1].strip() if ":" in c.section else c.section
            if title not in titles:
                titles.append(title)
        bullets = "\n".join(f"• {t}" for t in titles[:10])
        status = self._scenario_hint(req)
        if explicit_period:
            text = f"По чек-листу стажёра на период «{period}»:\n{bullets}"
        else:
            text = f"Сейчас ваш сценарий — получение доступа в пространство команды. {status}\n\nПо чек-листу на первую неделю также:\n{bullets}"
        text += "\n\nЧек-лист — материал кейса; конкретные договорённости команды уточняйте у наставника."
        hits = [Hit(chunk=c, score=1.0) for c in items[:6]]
        return TurnResult(text, "answer", "ask_plan", sources=self._src(hits))

    def _scenario_hint(self, req: TurnRequest) -> str:
        op = req.operation or {}
        ticket = op.get("ticket") or {}
        status = op.get("operation_status")
        if ticket.get("external_status") == "pending":
            return f"Заявка {ticket.get('ticket_id')} ждёт согласования — доступ ещё не выдан, пока изучите AvitoTech Playbook."
        if status in LOCKED_OPERATION_STATUSES:
            return "Отправка заявки не подтверждена — проверьте статус операции в карточке."
        if status == "awaiting_confirmation":
            return "Черновик заявки ждёт вашего подтверждения в карточке."
        return "Следующий шаг: оформить заявку на доступ к базе знаний своей команды."

    def _ask_open(self, req, slots, guard) -> TurnResult:
        """Открытый вопрос об инструментах/процессах: чек-лист + Playbook, с отказом при низкой уверенности."""
        res = self._search(req, req.message, None, k=5)
        threshold = self.settings.answer_min_confidence
        relevant = [h for h in res.hits if h.coverage >= threshold or (h.dense and h.dense >= 0.6)]
        if not relevant:
            return self._no_data(req, slots, guard)
        checklist = next((h for h in relevant if h.chunk.doc.doc_id in CHECKLIST_DOCS), None)
        main = next(
            (h for h in relevant if h.chunk.doc.doc_id not in CHECKLIST_DOCS and len(h.chunk.text) >= 120), None
        )
        parts, used = [], []
        if checklist:
            sec = checklist.chunk.section
            period, _, title = sec.partition(":")
            parts.append(f"В чек-листе стажёра (материал кейса) это пункт «{title.strip() or sec}», период — {period.strip()}.")
            used.append(checklist)
        if main:
            parts.append(f"{main.chunk.doc.source_label} — «{main.chunk.section}»:\n{_clip(main.chunk.text, 600)}")
            used.append(main)
        if not used:
            return self._no_data(req, slots, guard)
        parts.append(f"Как это устроено именно в вашей команде, лучше уточнить у наставника {req.user.mentor_handle}.".replace("  ", " "))
        return TurnResult("\n\n".join(parts), "answer", "ask_tools", sources=self._src(used))

    def _no_data(self, req, slots, guard) -> TurnResult:
        mentor = req.user.mentor_handle or "наставнику"
        text = (
            "В доступных мне тестовых инструкциях нет информации по этому вопросу, поэтому не буду придумывать "
            f"порядок действий. Лучше уточнить у наставника {mentor} — могу передать ему вопрос."
        )
        return TurnResult(text, "no_data", "out_of_scope", handoff_suggested=True)

    def _out_of_scope(self, req, slots, guard) -> TurnResult:
        # Неуверенный «вне темы» сначала проверяем поиском по ДОСТУПНЫМ пользователю документам:
        # так руководитель получает ответ из своего закрытого чек-листа, а стажёр — честный отказ.
        if req.route_confidence < 0.7:
            answer = self._ask_open(req, slots, guard)
            if answer.reply_kind == "answer":
                return answer
        return self._no_data(req, slots, guard)

    def _out_of_catalog(self, req, slots, guard) -> TurnResult:
        target = access_target(fuzzy_fix(normalize(req.message))) or "этому ресурсу"
        mentor = req.user.mentor_handle or "наставнику"
        text = (
            f"Порядок получения доступа к «{target}» в доступных тестовых инструкциях не описан, поэтому не буду его придумывать. "
            "Через помощника сейчас можно оформить доступ к пространству базы знаний и учебному репозиторию команды. "
            f"По «{target}» лучше обратиться к наставнику {mentor} — могу передать вопрос."
        )
        return TurnResult(text, "no_data", "out_of_catalog", handoff_suggested=True)

    def _handoff(self, req, slots, guard) -> TurnResult:
        mentor = req.user.mentor_handle or "наставнику"
        text = (
            f"Передам вопрос наставнику {mentor}. Нажмите «Обратиться к наставнику» — обращение сохранится "
            "с привязкой к текущей задаче, чтобы не пересказывать контекст."
        )
        return TurnResult(text, "handoff", "handoff", handoff_suggested=True)

    def _injection(self, req, slots, guard) -> TurnResult:
        flags = set(guard.flags)
        parts = [f"Роль и права берутся из вашего профиля ({req.user.role}, команда {req.user.team_name}) и не меняются через сообщения в чате."]
        if flags & {"prompt_exfiltration", "system_spoof", "override_instructions"} or "classifier" in flags:
            parts.append("Системные инструкции и закрытые документы я не раскрываю.")
        if "bypass_approval" in flags:
            parts.append("Заявка отправляется только после вашего подтверждения карточки, а доступ появляется только после согласования руководителем — обойти эти шаги нельзя.")
        if "foreign_data" in flags:
            parts.append("Заявки и данные других пользователей недоступны.")
        parts.append("Могу помочь оформить доступ в рамках ваших прав.")
        rules = self._section_hit(req.user, "doc_02_security_restrictions", "Матрица")
        return TurnResult(" ".join(parts), "denied", "injection", sources=self._src([rules] if rules else []))

    def _injection_soft(self, req, slots, guard) -> TurnResult:
        answer = self._ask_open(req, slots, guard)
        if answer.reply_kind == "answer":
            return answer
        guard.injection_detected = True
        guard.flags.append("classifier")
        return self._injection(req, slots, guard)

    def _check_status(self, req, slots, guard) -> TurnResult:
        op = req.operation or {}
        status = op.get("operation_status")
        ticket = op.get("ticket") or {}
        ext = ticket.get("external_status")
        tid = ticket.get("ticket_id")
        approver = req.user.approver_name or "руководитель команды"
        sources: list[Hit] = []
        handoff = False
        if not op:
            text = "У вас пока нет отправленных заявок по этой задаче."
            if req.draft.any():
                text += " Черновик ещё не отправлен — проверьте карточку и подтвердите отправку."
        elif status in ("draft", "awaiting_confirmation"):
            text = "Черновик заявки ещё не отправлен: проверьте параметры в карточке и подтвердите отправку."
        elif status == "submitting":
            text = "Заявка отправляется в сервис доступов — дождитесь результата, повторно нажимать не нужно."
        elif status == "unknown":
            text = ("Результат отправки не подтверждён: сервис мог сохранить заявку, но ответ потерялся. "
                    "Не создавайте новую заявку — нажмите «Проверить статус операции».")
        elif status == "failed":
            text = "Отправка не удалась, заявка не создана. Можно безопасно повторить отправку из карточки."
        elif ext == "approved":
            text = f"Заявка {tid} согласована руководителем. Фактическая выдача прав — отдельный этап; доступ появится после активации."
        elif ext == "provisioned":
            text = f"По заявке {tid} доступ выдан целевой системой."
        elif ext == "rejected":
            text = f"Заявка {tid} отклонена. Уточните причину у согласующего ({approver}) или наставника {req.user.mentor_handle}."
            handoff = True
        else:
            text = (f"Заявка {tid} создана и ожидает согласования (статус `pending`), согласующий — {approver}. "
                    "Доступ ещё не активирован. Стандартный срок рассмотрения по тестовой инструкции — до 4 рабочих часов.")
            hit = self._section_hit(req.user, "doc_01_onboarding_access", "Как происходит согласование")
            sources = [hit] if hit else []
        return TurnResult(text, "status", "check_status", sources=self._src(sources), handoff_suggested=handoff)

    # ------------------------------------------------------------ access request flow

    def _request_access(self, req: TurnRequest, new_slots: Slots, guard: GuardReport) -> TurnResult:
        user, op = req.user, (req.operation or {})
        prefix = ""
        if guard.injection_detected:
            prefix = f"Права определяются вашим профилем ({user.role}), а не текстом сообщения. "
        if op.get("operation_status") in LOCKED_OPERATION_STATUSES:
            text = prefix + ("Предыдущая отправка ещё не завершена или её результат не подтверждён. "
                             "Сначала проверьте статус операции в карточке — так мы не создадим дубликат заявки.")
            return TurnResult(text, "status", "request_access", slots=req.draft)

        slots = req.draft.merged(new_slots)
        # Если сменился ресурс — уровень и причина могли относиться к старому ресурсу.
        if new_slots.resource_id and req.draft.resource_id and new_slots.resource_id != req.draft.resource_id:
            slots = Slots(new_slots.resource_id, new_slots.access_level, new_slots.justification or req.draft.justification)

        ticket = op.get("ticket") or {}
        if (
            op.get("operation_status") == "succeeded"
            and ticket.get("external_status") == "pending"
            and ticket.get("resource_id") == slots.resource_id
            and (not slots.access_level or ticket.get("access_level") == slots.access_level)
        ):
            text = prefix + (f"Заявка {ticket.get('ticket_id')} на этот ресурс уже создана и ждёт согласования — "
                             "новую подавать не нужно. Доступ появится после согласования руководителем.")
            return TurnResult(text, "status", "request_access", slots=slots)

        if slots.resource_id:
            decision = check(user, req.permissions, slots.resource_id, slots.access_level)
            if not decision.allowed:
                return self._denied(req, slots, decision, prefix)

        missing = slots.missing()
        if missing:
            return self._clarify(req, slots, missing, prefix)

        args = {"resource_id": slots.resource_id, "access_level": slots.access_level, "justification": slots.justification}
        errors = validate_arguments(args)
        if errors:  # pragma: no cover - защищено missing(), оставлено как страховка
            return self._clarify(req, slots, ["justification"], prefix)
        resource = CATALOG[slots.resource_id]
        changed = req.draft.any() and not req.draft.missing() and req.draft.to_dict() != slots.to_dict()
        approver = user.approver_name or "руководителю команды"
        text = prefix + (
            ("Обновил черновик — подтвердите его заново. " if changed else "Подготовил черновик заявки. ")
            + f"Ресурс: {resource.title}; уровень: {level_ru(slots.access_level)}; обоснование: «{slots.justification}». "
            f"Проверьте карточку и подтвердите отправку — заявка уйдёт на согласование ({approver}). "
            "Доступ появится только после согласования."
        )
        hit = self._section_hit(user, "doc_01_onboarding_access", "Как происходит согласование")
        return TurnResult(
            text, "proposal", "request_access",
            sources=self._src([hit] if hit else []),
            slots=slots,
            tool_call={"name": PROPOSE_ACCESS_REQUEST["name"], "arguments": args},
            policy=check(user, req.permissions, slots.resource_id, slots.access_level).to_dict(),
        )

    def _denied(self, req: TurnRequest, slots: Slots, decision, prefix: str) -> TurnResult:
        user = req.user
        resource = CATALOG.get(slots.resource_id)
        matrix = self._section_hit(user, "doc_02_security_restrictions", "Матрица")
        blocked = self._section_hit(user, "doc_02_security_restrictions", "Действия при блокировке")
        sources = [h for h in (matrix,) if h]
        handoff = False
        if decision.code == "RBAC_ACCESS_DENIED":
            if resource and resource.kind == "prod_db":
                text = (f"Запросить доступ к ресурсу «{resource.title}» нельзя: по правилам ИБ стажёрам закрыт доступ "
                        "к промышленным базам данных и финансовым контурам, такой запрос блокируется на сервере. "
                        f"Если для задачи нужны данные оттуда, попросите наставника {user.mentor_handle} подготовить обезличенный дамп.")
                sources += [blocked] if blocked else []
            else:
                text = (f"Запросить доступ к ресурсу «{resource.title}» с вашей ролью нельзя: стажёр запрашивает учебные "
                        f"ресурсы и пространство своей команды. Если доступ действительно нужен для задачи — обсудите это с наставником {user.mentor_handle}.")
            handoff = True
            slots = Slots(None, None, slots.justification)
            kind = "denied"
        elif decision.code == "RBAC_LEVEL_NOT_ALLOWED":
            levels = ", ".join(level_ru(lv) for lv in decision.allowed_levels)
            text = (f"К ресурсу «{resource.title}» вам доступен только уровень {levels}: запрос "
                    f"{level_ru(slots.access_level)} будет отклонён системой контроля прав. "
                    "Если чтения достаточно — напишите «на чтение», и я подготовлю черновик.")
            slots = Slots(slots.resource_id, None, slots.justification)
            kind = "denied"
        elif decision.code == "LEVEL_UNSUPPORTED":
            text = ("Через помощника можно запросить только уровни read (чтение) или write (редактирование). "
                    "Какой из них нужен?")
            slots = Slots(slots.resource_id, None, slots.justification)
            kind = "clarification"
        else:  # pragma: no cover - ресурс всегда из каталога
            text = "Такого ресурса нет в каталоге. Уточните, к какому пространству или репозиторию нужен доступ."
            slots = Slots(None, slots.access_level, slots.justification)
            kind = "clarification"
        missing = slots.missing()
        return TurnResult(
            prefix + text, kind, "request_access", sources=self._src(sources), slots=slots,
            missing_slots=missing, expected_slot=missing[0] if missing else None,
            policy=decision.to_dict(), handoff_suggested=handoff,
        )

    def _clarify(self, req: TurnRequest, slots: Slots, missing: list[str], prefix: str) -> TurnResult:
        user = req.user
        known = []
        if slots.resource_id:
            known.append(f"ресурс — {CATALOG[slots.resource_id].title}")
        if slots.access_level:
            known.append(f"уровень — {level_ru(slots.access_level)}")
        if slots.justification and "justification" not in missing:
            known.append(f"обоснование — «{slots.justification}»")
        questions = []
        if "resource_id" in missing:
            options = [r for r in CATALOG.values() if req.permissions.levels(r.resource_id) and r.owner_team == user.team_id]
            hint = " или ".join(f"{r.title.lower()} (`{r.resource_id}`)" for r in options[:2])
            questions.append("к какому ресурсу нужен доступ" + (f" — например, {hint}" if hint else ""))
        if "access_level" in missing:
            questions.append("какой уровень нужен: чтение (read) — для изучения материалов или редактирование (write) — для заметок и отчётов")
        if "justification" in missing:
            if slots.justification and len(slots.justification) < 10:
                questions.append("обоснование слишком короткое — опишите задачу подробнее (10–300 символов)")
            else:
                questions.append("зачем нужен доступ — коротко, 10–300 символов, например «изучение документации команды по чек-листу первой недели»")
        text = prefix
        if known:
            text += "Записал: " + "; ".join(known) + ". "
        text += ("Уточните, пожалуйста, " + questions[0] + "." if len(questions) == 1
                 else "Чтобы подготовить заявку, уточните: " + "; ".join(f"{i + 1}) {q}" for i, q in enumerate(questions)) + ".")
        hit = self._section_hit(user, "doc_01_onboarding_access", "Какие параметры")
        return TurnResult(
            text, "clarification", "request_access", sources=self._src([hit] if hit else []),
            slots=slots, missing_slots=missing, expected_slot=missing[0],
        )

    # ------------------------------------------------------------ LLM mode

    def _llm_eligible(self, local: TurnResult) -> bool:
        # Отказы, «нет данных», статусы и инъекции отвечаем детерминированно: безопаснее и дешевле.
        return local.intent in QA_INTENTS | {"request_access", "greeting", "uncertain"} and local.reply_kind not in (
            "no_data", "status",
        )

    def _context_chunks(self, req: TurnRequest, local: TurnResult) -> list[Chunk]:
        res = self.retriever.search(req.message, req.user.role, req.user.team_id, k=4)
        chunks = [h.chunk for h in res.hits]
        for s in local.sources:
            c = next((c for c in self.kb.chunks if c.doc.doc_id == s["doc_id"] and c.section == s["section"]), None)
            if c and c not in chunks and c.doc.allows(req.user.role, req.user.team_id):
                chunks.append(c)
        return chunks[:6]

    def _system_prompt(self, req: TurnRequest, slots: Slots) -> str:
        perms = "; ".join(
            f"{rid} ({CATALOG[rid].title}): " + (", ".join(lv) if lv else "запрещено")
            for rid, lv in ((rid, req.permissions.levels(rid)) for rid in CATALOG)
        )
        op = req.operation or {}
        ticket = op.get("ticket") or {}
        operation = (f"{op.get('operation_status')}, заявка {ticket.get('ticket_id')} статус {ticket.get('external_status')}"
                     if op else "нет")
        return self.system_template.safe_substitute(
            user_name=req.user.name, role=req.user.role, team_name=req.user.team_name, team_id=req.user.team_id,
            mentor_handle=req.user.mentor_handle or "наставник", approver_name=req.user.approver_name or "руководитель команды",
            draft=str(slots.to_dict()), operation=operation, permissions=perms,
        )

    def _with_llm(self, req: TurnRequest, local: TurnResult) -> TurnResult:
        chunks = self._context_chunks(req, local)
        docs_block = []
        for c in chunks:
            text, _ = sanitize_document(c.text)
            docs_block.append(
                f'<document doc_id="{c.doc.doc_id}" section="{c.section}" type="{c.doc.source_label}">\n{text}\n</document>'
            )
        system = self._system_prompt(req, local.slots)
        messages = [{"role": "system", "content": system},
                    {"role": "system", "content": "<documents>\n" + "\n".join(docs_block) + "\n</documents>"}]
        messages += [{"role": h["role"], "content": h["content"]} for h in req.history[-6:]]
        messages.append({"role": "user", "content": req.message})
        resp = self.llm.chat(messages, tools=openai_tools())

        if resp.tool_calls:
            call = resp.tool_calls[0]
            if call.name != PROPOSE_ACCESS_REQUEST["name"]:
                return local
            return self._accept_llm_tool_call(req, local, call.arguments, resp.content)

        reply = (resp.content or "").strip()
        if not reply:
            return local
        if self.leaks(reply, req.user):
            return local
        reply, _ = enforce_status_truth(reply, ((req.operation or {}).get("ticket") or {}).get("external_status"))
        reply = re.sub(r"регламент\w*\s+авито", "тестовой инструкции прототипа", reply, flags=re.I)
        cited = {c.doc.doc_id for c in chunks}
        sources = [s for s in local.sources if s["doc_id"] in cited] or [
            Hit(chunk=c, score=0.0).to_source() for c in chunks[:2]
        ]
        if local.reply_kind == "proposal":
            # LLM ответил текстом там, где локальная логика предлагает черновик — оставляем
            # детерминированный черновик, текст берём от LLM.
            local.reply = reply
            return local
        local.reply = reply
        local.sources = sources if local.reply_kind != "clarification" else local.sources
        return local

    def _user_text(self, req: TurnRequest) -> str:
        return " ".join([h["content"] for h in req.history if h["role"] == "user"] + [req.message])

    def _accept_llm_tool_call(self, req: TurnRequest, local: TurnResult, args, content: str) -> TurnResult:
        if validate_arguments(args):
            return local
        user_text = normalize(self._user_text(req))
        said_level = extract_level(req.message) or req.draft.access_level or extract_level(user_text)
        said_resource = extract_resource(normalize(req.message), req.user) or req.draft.resource_id or extract_resource(user_text, req.user)
        just_tokens = content_tokens(args["justification"])
        grounded_just = bool(just_tokens) and len(just_tokens & content_tokens(user_text)) / len(just_tokens) >= 0.5
        if said_level != args["access_level"] or said_resource != args["resource_id"] or not grounded_just:
            # LLM подставил параметр, которого пользователь не называл, — не доверяем (раздел 8.1 ТЗ).
            return local
        slots = Slots(args["resource_id"], args["access_level"], args["justification"].strip())
        decision = check(req.user, req.permissions, slots.resource_id, slots.access_level)
        if not decision.allowed:
            return self._denied(req, slots, decision, "")
        result = self._request_access(
            TurnRequest(req.user, req.permissions, req.message, slots, req.operation, req.history), Slots(), GuardReport()
        )
        if content and result.tool_call:
            reply, _ = enforce_status_truth(content.strip(), None)
            if not self.leaks(reply, req.user):
                result.reply = reply
        result.guard = local.guard
        return result
