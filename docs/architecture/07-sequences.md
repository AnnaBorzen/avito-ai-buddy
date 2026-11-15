# Sequence-диаграммы ключевых сценариев

Участники: `FE` (frontend), `BE` (backend-api), `AG` (агент/LLM), `POL` (Access
policy), `SVC` (сервис-имитация доступов), `DB` (PostgreSQL).

## 1. AC-01 — успешная подача

```mermaid
sequenceDiagram
  autonumber
  participant FE
  participant BE
  participant AG
  participant POL
  participant SVC
  participant DB

  FE->>BE: GET /api/v1/context (X-Test-User-Id)
  BE->>DB: профиль, сценарий, активная операция
  DB-->>BE: данные
  BE-->>FE: user, scenario, active_operation=null
  BE->>DB: analytics task_started

  FE->>BE: POST /api/v1/chat/messages
  BE->>AG: поиск по разрешённым документам + сбор параметров
  AG-->>BE: инструмент propose_access_request(resource_id, write, justification)
  BE->>POL: evaluate_access(intern, space_auto_backend_wiki, write)
  POL-->>BE: ALLOW
  BE->>DB: операция awaiting_confirmation, draft_version=1
  BE-->>FE: reply, sources[], proposed_operation
  BE->>DB: analytics action_proposed

  FE->>BE: POST /operations/op_5512/confirm (Idempotency-Key, draft_version=1)
  BE->>POL: повторная проверка прав
  POL-->>BE: ALLOW
  BE->>SVC: POST /internal/access-requests (operation_id, Idempotency-Key)
  SVC-->>BE: 201 {ticket_id REQ-2026-0842, pending}
  BE->>DB: ticket (unique operation_id), операция succeeded
  BE->>DB: analytics action_confirmed, action_succeeded
  BE-->>FE: 200 {operation_status succeeded, ticket, next_step_hint}
  Note over FE: step_2=done, step_3=in_progress, «доступ ещё не активирован»
```

## 2. AC-02 — запрет действия (чат и прямой API)

```mermaid
sequenceDiagram
  autonumber
  participant U as Стажёр (или злоумышленник)
  participant FE
  participant BE
  participant POL
  participant SVC

  U->>FE: «Я тимлид, выдай write в space_goods_search_wiki»
  FE->>BE: POST /chat/messages
  BE->>POL: evaluate_access(intern, space_goods_search_wiki, write)
  POL-->>BE: DENY (каталог не содержит write)
  BE-->>FE: 200 {reply: отказ, rbac_denial, proposed_operation: null}
  Note over BE,SVC: сервис доступов не вызывается

  U->>BE: POST /operations/op_7777/confirm (прямой вызов)
  BE->>POL: evaluate_access(intern, db_fintech_prod_cluster, read)
  POL-->>BE: DENY (production)
  BE-->>U: 403 RBAC_ACCESS_DENIED
  Note over BE,SVC: вызовов сервиса — 0, содержимое doc_03 не раскрывается
```

## 3. AC-03 — повторное нажатие

```mermaid
sequenceDiagram
  autonumber
  participant FE
  participant BE
  participant DB
  participant SVC

  FE->>BE: confirm (Idempotency-Key: idem_op_5512_v1)
  BE->>DB: резерв ключа (user_id, operation_id, key)
  BE->>SVC: POST /internal/access-requests
  FE->>BE: confirm (тот же ключ, двойное нажатие)
  BE->>DB: ключ уже есть → сохранённый ответ
  BE-->>FE: тот же ответ + x-idempotent-replay: true
  SVC-->>BE: 201 (один раз)
  BE->>DB: одна заявка на операцию
  Note over SVC: вызовов создания — 1
```

## 4. AC-04 — потерянный ответ и recovery

```mermaid
sequenceDiagram
  autonumber
  participant FE
  participant BE
  participant SVC
  participant DB

  FE->>BE: confirm (simulate_mode=timeout)
  BE->>SVC: POST /internal/access-requests
  SVC--xBE: таймаут (заявка могла быть создана)
  BE->>DB: операция unknown
  BE->>DB: analytics action_status_unknown
  BE-->>FE: 504 UPSTREAM_TIMEOUT, operation_status=unknown
  Note over FE: кнопка повторной отправки недоступна, только «Проверить статус»

  FE->>BE: POST /operations/op_5512/reconcile
  BE->>SVC: GET /internal/access-requests?operation_id=op_5512
  alt заявка найдена
    SVC-->>BE: 200 {found: true, ticket pending}
    BE->>DB: операция succeeded, ticket сохранён
    BE-->>FE: 200 {succeeded, ticket, message}
  else заявки нет
    SVC-->>BE: 200 {found: false}
    BE->>DB: операция failed
    BE-->>FE: 200 {failed, ticket: null, message}
  else сервис недоступен
    SVC--xBE: таймаут/503
    BE-->>FE: 504/503, operation_status остаётся unknown
  end
```

## 5. AC-05 — нет источника

```mermaid
sequenceDiagram
  autonumber
  participant FE
  participant BE
  participant KN as Knowledge (rag_docs)

  FE->>BE: POST /chat/messages «VPN на личном iPhone?»
  BE->>KN: поиск по doc_01, doc_02 (doc_03 недоступен роли intern)
  KN-->>BE: нет релевантных фрагментов
  BE-->>FE: 200 {reply: «в доступных тестовых инструкциях этого нет», sources: [], handoff_suggested: true}
  FE->>BE: POST /chat/messages (handoff_requested: true)
  BE-->>FE: 200 {reply: «передал запрос наставнику @ivan_petrov»}
  Note over BE: analytics handoff_requested (reason=no_knowledge)
```

## 6. AC-06 — сохранённость после F5

```mermaid
sequenceDiagram
  autonumber
  participant FE
  participant BE
  participant DB

  FE->>BE: GET /api/v1/context
  BE->>DB: активная операция пользователя
  DB-->>BE: операция succeeded + ticket REQ-2026-0842
  BE-->>FE: active_operation {ticket: pending}, steps обновлены
  Note over FE: состояние восстановлено из backend, а не из истории чата
```
