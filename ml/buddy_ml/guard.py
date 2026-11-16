"""Guardrails: prompt-injection, утечки закрытых документов и ложные заявления об успехе.

Принципы (раздел 5 ТЗ):
  * идентичность и права — только из серверного контекста; «я руководитель» в чате их не меняет;
  * документы — данные, а не инструкции: перед LLM они санитизируются и оборачиваются в теги;
  * успех сообщается только по подтверждённому результату инструмента;
  * pending никогда не превращается в «доступ выдан»;
  * содержимое документов, закрытых для пользователя, не попадает в ответ.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field

from .knowledge import Chunk
from .text import content_tokens, normalize

_INJECTION_PATTERNS: dict[str, re.Pattern] = {
    "override_instructions": re.compile(
        r"игнорир\w*\s+(все\s+)?(предыдущ\w*|прошл\w*|сво\w*|эти)?\s*(инструкц|правил|ограничен)|"
        r"забудь\s+(все\s+)?(правил|инструкц|ограничен)|ignore\s+(all\s+)?(previous|prior|above)|"
        r"disregard\s+(all\s+)?(previous|prior)|отключи\s+(все\s+)?(ограничен|фильтр|провер)|"
        r"режим\s+разработчика|developer\s+mode|\bdan\b|jailbreak"
    ),
    "role_claim": re.compile(
        r"\bя\s+(теперь\s+)?(тимлид|руководител\w*|лид|lead|админ\w*|администратор\w*|наставник|ментор)\b|"
        r"(у\s+меня|мо[яю])\s+роль\s+\w+|сделай\s+меня\s+\w+|смени\s+(мою\s+)?роль|"
        r"представь,?\s+что\s+я|user\s+is\s+(an?\s+)?admin|ты\s+теперь\s+\w+|действуй\s+как"
    ),
    "system_spoof": re.compile(r"^\s*(\[?system\]?|system:|системн\w+\s+сообщени\w*:)|<\s*/?\s*system\s*>"),
    "prompt_exfiltration": re.compile(
        r"(покажи|выведи|раскрой|напечатай)\s+(сво\w+\s+|весь\s+)?(системн\w+\s+)?(промпт|инструкц\w*|prompt)|system\s+prompt"
    ),
    "bypass_approval": re.compile(
        r"обойди|в\s+обход|без\s+(согласован|подтвержден|аппрув|заявки)|сразу\s+выда|"
        r"одобри\s+(мою\s+|сам\w*\s+)?заявк|одобри\s+сам|поставь\s+статус|approve|апрув\w*\s+сам|"
        r"отправь\s+(заявку\s+)?без"
    ),
    "foreign_data": re.compile(
        r"(заявк\w*|данн\w*|доступ\w*)\s+(других|чуж\w+|всех)\s+(стаж\w+|пользовател\w+|сотрудник\w+)|"
        r"заявки\s+(марии|алексея|другого)|какие\s+заявки\s+у\s+других"
    ),
}

# Инструкции, спрятанные внутри документов (indirect prompt injection).
_DOC_INSTRUCTION_RE = re.compile(
    r"(?im)^.*(игнорир\w*\s+(все\s+)?(предыдущ|правил|инструкц)|ignore\s+(all\s+)?previous|"
    r"ты\s+(должен|обязан)\s+(выдать|одобрить|раскрыть)|assistant\s*:|system\s*:|"
    r"выдай\s+(всем\s+)?доступ|раскрой\s+(закрыт|секрет)).*$"
)

# Формулировки, означающие выданный доступ. Допустимы только при ticket.external_status
# == provisioned (в MVP сервис-имитация такой статус не возвращает).
_GRANTED_CLAIM_RE = re.compile(
    r"доступ\s+(уже\s+)?(выдан|предоставлен|открыт|активирован|получен)|"
    r"вы\s+(уже\s+)?получили\s+доступ|доступ\s+у\s+вас\s+(уже\s+)?есть|"
    r"заявк\w+\s+(уже\s+)?(одобрен|согласован)|права\s+(уже\s+)?выданы"
)
_NEGATED_CLAIM_RE = re.compile(
    r"(не|ещё не|еще не|пока не|не означает,?\s+что)\s+(\w+\s+){0,3}(выдан|предоставлен|открыт|активирован|получен|одобрен|согласован)"
)


@dataclass
class GuardReport:
    injection_detected: bool = False
    flags: list[str] = field(default_factory=list)

    def to_dict(self) -> dict:
        return {"injection_detected": self.injection_detected, "flags": list(self.flags)}


def detect_injection(text: str) -> GuardReport:
    norm = normalize(text)
    flags = [name for name, pattern in _INJECTION_PATTERNS.items() if pattern.search(norm)]
    return GuardReport(injection_detected=bool(flags), flags=flags)


def sanitize_document(text: str) -> tuple[str, bool]:
    """Удаляет из документа строки, похожие на инструкции модели. Возвращает (текст, было_ли)."""
    cleaned, n = _DOC_INSTRUCTION_RE.subn("[удалено: инструкция внутри документа]", text)
    return cleaned, n > 0


def leaks_forbidden(
    reply: str, forbidden: list[Chunk], threshold: float = 0.5, allowed_vocab: set[str] | None = None
) -> list[str]:
    """Doc_id закрытых документов, чьё содержимое заметно пересекается с ответом.

    Считаются только стемы, которых НЕТ в документах, доступных пользователю: общая лексика
    («период прохождения», «неделя») не является утечкой. Плюс точечные маркеры (числа
    рядом с «лицензия/квота»).
    """
    reply_tokens = content_tokens(reply)
    reply_numbers = set(re.findall(r"\d+", reply))
    allowed_vocab = allowed_vocab or set()
    leaked = []
    for chunk in forbidden:
        tokens = content_tokens(chunk.text) - allowed_vocab
        if len(tokens) < 3:
            continue  # у фрагмента нет собственного «секретного» содержимого
        overlap = len(tokens & reply_tokens) / len(tokens)
        numbers = set(re.findall(r"\d+", chunk.text))
        rare_hit = bool(numbers & reply_numbers) and bool(
            {"лиценз", "квот", "квартал"} & reply_tokens
        )
        if overlap >= threshold or rare_hit:
            leaked.append(chunk.doc.doc_id)
    return sorted(set(leaked))


def vocabulary(chunks: list[Chunk]) -> set[str]:
    vocab: set[str] = set()
    for c in chunks:
        vocab |= content_tokens(c.section + " " + c.text)
    return vocab


def claims_access_granted(reply: str) -> bool:
    norm = normalize(reply)
    for sentence in re.split(r"(?<=[.!?])\s+", norm):
        if _GRANTED_CLAIM_RE.search(sentence) and not _NEGATED_CLAIM_RE.search(sentence):
            return True
    return False


def enforce_status_truth(reply: str, ticket_status: str | None) -> tuple[str, bool]:
    """Если тикет не в provisioned, убирает ложные заявления о выданном доступе."""
    if ticket_status == "provisioned" or not claims_access_granted(reply):
        return reply, False
    kept = [
        s
        for s in re.split(r"(?<=[.!?])\s+", reply)
        if not (_GRANTED_CLAIM_RE.search(normalize(s)) and not _NEGATED_CLAIM_RE.search(normalize(s)))
    ]
    note = "Создание заявки не означает выдачу доступа: доступ появится только после согласования руководителем."
    return (" ".join(kept + [note])).strip(), True
