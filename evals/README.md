# Evals

Синтетические задания для проверки поведения агента и API на сценариях
приёмки [`specs/access-request.md`](../specs/access-request.md) §12.

## Структура

| Каталог | Назначение |
|---|---|
| `tasks/` | входные задания: сообщение пользователя, профиль, контекст |
| `expected/` | эталоны: ожидаемое состояние операции, состав вызовов инструментов, события |
| `runs/` | результаты прогонов (содержимое не коммитится, кроме `.gitkeep`) |

## Формат задания

```json
{
  "id": "ac-01-happy-path",
  "description": "Стажёр просит write в пространство своей команды",
  "input": {
    "user_id": "usr_intern_01",
    "task_instance_id": "tsk_901",
    "message": "Оформи мне доступ на запись в базу знаний нашей команды Auto Backend"
  },
  "expected": {
    "operation_status": "awaiting_confirmation",
    "tool_calls": ["search_knowledge", "get_resource_policy", "propose_access_request"],
    "events": ["task_started", "answer_delivered", "action_proposed"]
  }
}
```

Имена инструментов — только из [`contracts/mcp-tools.yaml`](../contracts/mcp-tools.yaml),
имена событий — только из [`docs/metrics.md`](../docs/metrics.md) (§2), статусы —
только из enum-ов [`contracts/api.yaml`](../contracts/api.yaml).

## Обязательный набор

| Задание | Сценарий | Что проверяется |
|---|---|---|
| `ac-01` | успешная подача | создаётся одна заявка, `pending`, следующий шаг не обещает доступ |
| `ac-02` | запрет в чате | `rbac_denial`, ни одного `action_confirmed` |
| `ac-02-api` | запрет через прямой API | 403 `RBAC_ACCESS_DENIED`, сервис не вызывается |
| `ac-03` | двойное нажатие | один вызов сервиса, один `action_succeeded` |
| `ac-04` | таймаут + сверка | `action_status_unknown`, затем восстановление |
| `ac-05` | нет источника | пустые/нерелевантные `sources`, `handoff_suggested: true` |
| `ac-05-control` | контрольный вопрос про VPN | ответ не выдумывает процедуру |
| `ac-06` | F5 после создания | состояние восстанавливается из `GET /context` |

## План

1. Держать задания в соответствии с текущей версией спецификации.
2. Runner (Python или Go) прогоняет задания против поднятого API (демо-макет или
   `backend-api`) и сравнивает статусы, вызовы инструментов и события.
3. Результаты складывать в `runs/<date>/` и прикладывать к отчёту покрытия.
