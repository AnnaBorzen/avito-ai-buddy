"""Схемы инструментов агента (Function Calling / MCP) и их валидация.

Агенту доступен только `propose_access_request` — он создаёт ЧЕРНОВИК. Отправка заявки
(`confirm`) выполняется backend'ом исключительно по явному подтверждению в UI (FR-05),
поэтому соответствующего инструмента у LLM нет вообще.

Поля user_id / team_id / approver_id намеренно отсутствуют в параметрах: backend
подставляет их из серверной сессии (защита от подмены через промпт).
"""

from __future__ import annotations

from .policy import CATALOG, TOOL_LEVELS

PROPOSE_ACCESS_REQUEST = {
    "name": "propose_access_request",
    "description": (
        "Создаёт черновик (draft) заявки на доступ к корпоративному ресурсу для предпросмотра "
        "и подтверждения стажёром. Не выполняет реальную отправку в сервис."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "resource_id": {
                "type": "string",
                "enum": list(CATALOG),
                "description": "Точный идентификатор ресурса из каталога.",
            },
            "access_level": {
                "type": "string",
                "enum": list(TOOL_LEVELS),
                "description": (
                    "Запрашиваемый уровень доступа (read — чтение, write — редактирование). "
                    "Должен быть явно назван пользователем."
                ),
            },
            "justification": {
                "type": "string",
                "minLength": 10,
                "maxLength": 300,
                "description": "Деловое обоснование запроса со слов пользователя (зачем нужен доступ).",
            },
        },
        "required": ["resource_id", "access_level", "justification"],
        "additionalProperties": False,
    },
}

TOOLS = [PROPOSE_ACCESS_REQUEST]


def openai_tools() -> list[dict]:
    return [{"type": "function", "function": t} for t in TOOLS]


def validate_arguments(args: object) -> list[str]:
    """Возвращает список ошибок валидации (пустой — аргументы корректны)."""
    if not isinstance(args, dict):
        return ["arguments must be an object"]
    schema = PROPOSE_ACCESS_REQUEST["parameters"]
    errors = []
    for key in schema["required"]:
        if key not in args or args[key] in (None, ""):
            errors.append(f"missing required field: {key}")
    for key in args:
        if key not in schema["properties"]:
            errors.append(f"unexpected field: {key}")
    if "resource_id" in args and args["resource_id"] not in CATALOG:
        errors.append("resource_id is not in catalog")
    if "access_level" in args and args["access_level"] not in TOOL_LEVELS:
        errors.append("access_level must be read or write")
    just = args.get("justification")
    if just is not None and not (isinstance(just, str) and 10 <= len(just.strip()) <= 300):
        errors.append("justification length must be 10..300")
    return errors
