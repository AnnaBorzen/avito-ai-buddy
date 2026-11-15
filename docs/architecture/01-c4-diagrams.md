# C4-диаграммы AI-buddy

Модель C4: L1 (System Context), L2 (Container), L3 (Component backend).
Обозначения: сплошная стрелка — синхронный вызов, пунктир — «предполагается в
проде»/имитация.

## L1 — System Context

```mermaid
flowchart LR
  intern["Стажёр (intern)<br/>usr_intern_01"]
  mentor["Наставник<br/>@ivan_petrov"]
  lead["Тимлид (lead)<br/>usr_lead_01"]

  buddy["AI-buddy для стажёров<br/>чат + карточка заявки"]
  kb[("Синтетическая база знаний<br/>rag_docs, 3 документа")]
  access["Сервис-имитация доступов<br/>владелец заявки"]
  llm["Провайдер LLM<br/>(внешний контур)"]

  intern -->|"спрашивает порядок, подтверждает заявку"| buddy
  buddy -->|"ищет только разрешённые документы"| kb
  buddy -->|"создаёт заявку после подтверждения"| access
  buddy -->|"формирует ответ по источникам"| llm
  buddy -->|"показывает ID, статус, следующий шаг"| intern
  lead -->|"согласует заявку (вне MVP-экрана)"| access
  buddy -.->|"предлагает обратиться при нехватке знаний"| mentor
```

Граница системы: помощник **не** выдаёт доступ и не согласует заявку. Он доводит
пользователя до проверяемого результата — созданной и сохранённой заявки
(§1 ТЗ, спека §1).

## L2 — Containers

```mermaid
flowchart TB
  subgraph client["Клиент"]
    spa["Frontend SPA<br/>React 18 + Vite + TS<br/>чат, карточка, шаги"]
  end

  subgraph platform["Контур AI-buddy"]
    api["backend-api<br/>Go, net/http<br/>контекст, чат, операции, RBAC"]
    db[("PostgreSQL 17<br/>операции, заявки, события")]
    cache[("Redis 7.4<br/>кэш профилей и справочника")]
  end

  subgraph simulated["Имитации внешних систем"]
    svc["access-service mock<br/>заявки на доступ"]
    rag[("RAG-корпус<br/>rag_docs/*.md")]
  end

  model["LLM provider"]

  spa -->|"JSON /api/v1<br/>X-Test-User-Id"| api
  api --> db
  api --> cache
  api -->|"фильтр по роли/команде"| rag
  api -->|"propose_access_request / ответ"| model
  api -->|"создание заявки, сверка"| svc
  svc -->|"ticket_id, external_status"| api

  demo["Демо-макет API<br/>frontend/server/*.mjs<br/>Vite middleware + node:sqlite"]
  spa -.->|"VITE_USE_MOCK_API != false"| demo
  demo -.->|"тот же contracts/api.yaml"| svc
```

Правило: и `backend-api`, и демо-макет обязаны соответствовать
[`contracts/api.yaml`](../../contracts/api.yaml). Расхождение — дефект реализации,
а не контракта (см. [`08-validation-checklist.md`](08-validation-checklist.md)).

## L3 — Компоненты backend-api

```mermaid
flowchart LR
  h["transport/http<br/>router, middleware,<br/>единый маппинг ошибок"]
  ctl["Контекст и профили<br/>profiles, teams"]
  chat["Chat / Agent<br/>сбор параметров, ответ по источникам"]
  tools["Tool layer (MCP)<br/>propose_access_request,<br/>search_knowledge, get_resource_policy"]
  pol["Access policy<br/>evaluate_access()"]
  ops["Operation service<br/>draft_version, confirm,<br/>идемпотентность, reconcile"]
  ad["Access-service adapter<br/>timeout/ошибка/успех → статус"]
  repo["Repository<br/>операции, заявки, идемпотентность"]
  an["Analytics<br/>AnalyticsEvent"]
  kn["Knowledge<br/>rag_docs + фильтр"]

  h --> ctl
  h --> chat
  h --> ops
  chat --> tools
  chat --> kn
  tools --> pol
  tools --> ops
  ops --> pol
  ops --> repo
  ops --> ad
  ops --> an
  chat --> an
```

Владельцы правил (единственная политика на правило):

| Правило | Владелец |
|---|---|
| доступность ресурса/уровня | `Access policy` (спека §11.3) |
| жизненный цикл операции и `draft_version` | `Operation service` |
| идемпотентность `confirm` | `Operation service` + уникальные ключи в `Repository` |
| различение «не дошло / неизвестно» | `Access-service adapter` (спека §13.1) |
| фильтрация документов | `Knowledge` (спека §8) |
| формат ответа об ошибке | `transport/http` (04-error-model) |
