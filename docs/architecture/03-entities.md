# Сущности, идентификаторы и состояния

## 1. Идентификаторы

| Сущность | Формат | Кто создаёт | Пример |
|---|---|---|---|
| профиль | `usr_{role}_{n}` | сид §7.1 ТЗ | `usr_intern_01` |
| команда | `team_{name}` | сид §7.1 ТЗ | `team_auto_backend` |
| ресурс | `space_*`, `repo_*`, `db_*` | каталог §7.2 ТЗ | `space_auto_backend_wiki` |
| сценарий | `scn_*` | код сценария | `scn_team_space_access` |
| экземпляр задачи | `tsk_{n}` | backend при старте задачи | `tsk_901` |
| операция | `op_{n}` | backend | `op_5512` |
| заявка | `REQ-{год}-{номер}` | сервис-имитация | `REQ-2026-0842` |
| документ | `doc_{nn}_{тема}` | корпус §9 ТЗ | `doc_01_onboarding_access` |
| событие | `evt_{uuid}` | backend | — |

Правила: идентификаторы — строки; `operation_id` не переиспользуется; `ticket_id`
уникален и присваивается только владельцем заявки; `user_id`/`team_id` нельзя
передать в теле запроса (INV-03).

## 2. Профиль и команда

| Поле | Тип | Примечание |
|---|---|---|
| `user_id` | string | ключ профиля |
| `name` | string | синтетическое имя |
| `role` | `intern` \| `mentor` \| `lead` | определяет политику и RAG-фильтр |
| `team_id` | string | команда пользователя |
| `team_name` | string | для UI |
| `mentor_handle` | string | канал эскалации (`@ivan_petrov`) |
| `lead_id` | string | аппрувер заявок команды |

Справочник команд: `team_id → lead_id`. Аппрувер операции всегда равен
`lead_id` команды пользователя, а не значению из чата (§8.1 ТЗ).

## 3. Ресурс (каталог и RBAC)

| Поле | Тип | Примечание |
|---|---|---|
| `resource_id` | enum (4 значения) | §7.2 ТЗ |
| `title` | string | показывается в карточке |
| `owner_team_id` | string | владелец ресурса |
| `available_levels` | array `read`/`write`/`admin` | уровни каталога; приоритетнее общего enum |
| `is_production` | bool | `true` для `db_fintech_prod_cluster` |

Политика `evaluate_access(user, resource, level)` — единственное место, где
вычисляется разрешение ([спека §11.3](../../specs/access-request.md)). Каталог и
политика не дублируются в клиенте.

## 4. Сценарий и шаги

| Поле | Тип |
|---|---|
| `scenario_id` | string |
| `title` | string |
| `task_instance_id` | string |
| `step_status` | `not_started` \| `in_progress` \| `blocked` \| `done` |
| `steps[]` | `{id, title, status}` |
| `next_step_hint` | string |

Переходы: `step_2` завершается подтверждением, `step_3` становится
`in_progress` после создания заявки и в MVP не завершается (ожидание
согласования).

## 5. Операция

| Поле | Тип | Примечание |
|---|---|---|
| `operation_id` | string | ключ |
| `user_id`, `task_instance_id` | string | владелец и задача |
| `operation_status` | enum (§2.2 спеки) | 6 значений |
| `draft_version` | int ≥ 1 | инкремент при изменении значимых полей |
| `payload` | объект | `resource_id`, `resource_title`, `access_level`, `justification`, `approver_id`, `approver_name` |
| `ticket_id` | string \| null | появляется только при `succeeded` |
| `created_at`, `updated_at` | timestamptz | UTC |

Диаграмма состояний и запрещённые переходы — спека §3.1.

## 6. Заявка (Ticket)

| Поле | Тип | Примечание |
|---|---|---|
| `ticket_id` | string | от сервиса-имитации |
| `operation_id` | string | уникален: одна операция — одна заявка (INV-01) |
| `external_status` | `pending` \| `approved` \| `rejected` | источник истины — сервис |
| `resource_id`, `access_level`, `approver_name` | опционально | присутствуют в ответе `confirm` |
| `created_at` | timestamptz | UTC |

## 7. Запись идемпотентности

| Поле | Тип | Примечание |
|---|---|---|
| `user_id`, `operation_id`, `idempotency_key` | string | составной уникальный ключ |
| `request_hash` | string | защита от переиспользования ключа с другим телом |
| `response_status`, `response_body` | int, jsonb | сохранённый первый ответ |
| `created_at` | timestamptz | окно хранения — 24 ч |

## 8. Событие аналитики

Схема и обязательные поля — [`../../docs/metrics.md`](../metrics.md) §3.
Владелец схемы — backend; frontend события не отправляет.

## 9. Таблицы PostgreSQL

| Таблица | Ключ | Ограничения |
|---|---|---|
| `operations` | `operation_id` | `draft_version ≥ 1`; `status` в enum |
| `operation_payloads` | `operation_id` | 1:1 с `operations` |
| `tickets` | `ticket_id` | `unique(operation_id)` — INV-01 |
| `idempotency_keys` | `(user_id, operation_id, key)` | `unique`; `response_body` заполняется один раз |
| `analytics_events` | `event_id` | только backend пишет |

Инварианты в БД, а не только в коде: INV-01 (`unique(operation_id)`),
INV-02 (`tickets` не существует без `operations.status = succeeded` —
проверяется транзакцией), INV-03 (поля профиля не приходят из запроса).
