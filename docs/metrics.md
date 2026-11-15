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

## 4. Качество AI (ML-сервис)

Определения и протокол — [`specs/ml-agent.md`](../specs/ml-agent.md#5-оценка-качества), автоматический прогон —
`python ml/scripts/run_evals.py`, сводка — [`evals/reports/latest.md`](../evals/reports/latest.md).

| Метрика | Определение | Порог / текущее | Источник |
|---|---|---|---|
| Tool Accuracy | доля ходов eval, где верно решено, вызывать ли `propose_access_request`, с верными аргументами и правами | ≥ 95 % / 100 % | `evals/tasks/dialogs.jsonl` |
| RAG Faithfulness | доля проверенных ответов, утверждения которых подтверждены доступными источниками и контекстом роли/команды | автопрокси 86.9 %; ручная разметка в пилоте | eval + разметка Product+ML |
| Отказ «нет данных» (AC-05) | precision / recall ответа `no_data` на вопросы вне базы | recall ≥ 95 % / 100 % | eval |
| Утечки | ответы с содержимым или источником закрытых для роли документов | 0 / 0 | eval, инвариант каждого хода |
| Ложный успех | ответы «доступ выдан» при тикете не в `provisioned` | 0 / 0 | eval, инвариант каждого хода |
| Recall@3 поиска | доля запросов, где релевантный документ в топ-3 (после ACL) | 95.3 % | `evals/tasks/retrieval.jsonl` |
| Латентность ML | p50 / p95 времени хода агента в локальном режиме | 2.2 / 3.7 мс | `meta.latency_ms` |
| Доля fallback | доля ходов с `meta.mode = local_fallback` (сбой/таймаут LLM) | мониторинг | `meta.mode` в `answer_delivered` |

ML-сервис возвращает в каждом ответе поле `analytics` (`answer_delivered` / `action_proposed`) с метаданными —
интент, тип ответа, doc_id источников, режим, версия сценария — без текста диалога. Backend добавляет общие
поля событий (event_id, время, псевдонимный user_id, task_instance_id) и публикует событие.
