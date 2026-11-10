# Метрики и события

> Статус: черновик. События ещё не отправляются — каркас фиксирует контракт,
> чтобы backend и агент писали одинаковые данные с первой итерации.

## 1. Продуктовые метрики

| Метрика | Определение | Целевое значение | Источник |
|---|---|---|---|
| Time to access | Время от `access_request_started` до `access_request_approved` | TODO | события заявки |
| Share of self-served requests | Доля заявок без участия поддержки | TODO | события + тикеты |
| Clarification rounds | Среднее число уточнений до отправки заявки | TODO | `access_request_field_collected` |
| Rejection rate | Доля заявок, отклонённых ответственным | TODO | `access_request_rejected` |
| Agent error rate | Доля диалогов, завершившихся ошибкой инструмента | TODO | `agent_tool_failed` |

## 2. События

Формат: JSON. Обязательные поля для всех событий: `event`, `ts` (RFC 3339),
`request_id`, `user_id`, `payload`.

| Событие | Когда отправляется | Обязательные поля payload |
|---|---|---|
| `access_request_started` | сотрудник начал диалог | `system_hint` |
| `access_request_field_collected` | агент зафиксировал поле | `field`, `source` |
| `access_request_validation_failed` | не прошла проверка политики/полей | `reason` |
| `access_request_submitted` | заявка передана ответственному | `approver_id` |
| `access_request_approved` | заявка одобрена | `approver_id` |
| `access_request_rejected` | заявка отклонена | `approver_id`, `reason` |
| `agent_tool_failed` | инструмент агента вернул ошибку | `tool`, `error` |

## 3. План проверки

- Синтетические сценарии из `evals/tasks/` прогоняются на staging перед релизом.
- События сверяются с ожидаемыми в `evals/expected/` (наличие, обязательные поля).
- Дашборд Grafana (в планах, см. `infra/README.md`): заявки по статусам,
  p95 времени ответа API, доля ошибок инструментов.
- Продуктовые события дублируются в Clickstream для сквозной воронки.
