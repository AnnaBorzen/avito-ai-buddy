# Avito AI Buddy

AI-помощник для стажёра: помогает разобраться в порядке запроса доступа, собрать
валидную заявку, показать её предпросмотр, после явного подтверждения создать
заявку в сервисе-имитации и честно показать её ID, статус и следующий шаг.

MVP — один глубоко проработанный сценарий: **запрос доступа в пространство своей
команды**. Помощник не выдаёт доступы и не заменяет наставника; созданная заявка
не равна согласованной заявке и не равна выданному доступу.

## Документы

| Документ | Что внутри |
|---|---|
| [`docs/tz/README.md`](docs/tz/README.md) | эталон `Авито_ТЗ.docx`: версия, md5, карта разделов; сам текст — [`docs/tz/avito-tz.md`](docs/tz/avito-tz.md) |
| [`docs/PRD.md`](docs/PRD.md) | цель, пользователь, состав MVP, FR-01…FR-12, интерфейс, DoD |
| [`specs/access-request.md`](specs/access-request.md) | поведение AI и сервера: три словаря статусов, инварианты, RBAC, AC-01…AC-06, ошибки, идемпотентность, сверка |
| [`contracts/api.yaml`](contracts/api.yaml) | OpenAPI 3.1: `/api/v1/context`, `/chat/messages`, `/confirm`, `/reconcile` |
| [`contracts/mcp-tools.yaml`](contracts/mcp-tools.yaml) | инструменты агента: `propose_access_request` + читающие расширения |
| [`contracts/access-service.openapi.yaml`](contracts/access-service.openapi.yaml) | внутренний контракт сервиса-имитации доступов |
| [`docs/architecture.md`](docs/architecture.md) | индекс архитектурных решений и [`docs/architecture/01…08`](docs/architecture) |
| [`docs/metrics.md`](docs/metrics.md) | метрики, 9 событий, общие поля, единица анализа |
| [`docs/rag-corpus.md`](docs/rag-corpus.md) | синтетическая база знаний: front-matter, маркеры, правила правки (сами документы — `rag_docs/`) |
| [`evals/README.md`](evals/README.md) | синтетические задания и эталоны под AC-01…AC-06 |

Порядок чтения: PRD → спецификация → контракты → архитектура.

## Стек

| Слой | Технологии |
|---|---|
| Backend (целевой) | Go 1.26, `net/http`, `log/slog`, `koanf` |
| Демо-API | Vite middleware + `node:sqlite` (`frontend/server/`) |
| Тесты фронтенда | Vitest + Testing Library (порог покрытия 81% строк) |
| Backend | Go 1.26, `net/http`, `log/slog`, `koanf` (env-конфиг) |
| Frontend | React 18, TypeScript, Vite |
| ML-сервис | Python 3.12, Flask + gunicorn, scikit-learn, snowball-стемминг; опц. sentence-transformers, MCP SDK |
| Данные | PostgreSQL 17, Redis 7.4 |
| Инфраструктура | Docker, Docker Compose, GitHub Actions |

## Быстрый старт (Docker)

```bash
cp .env.example .env      # при необходимости поменяйте порты и пароли
make up                   # docker compose up -d --build
make db-migrate-up        # схема operations/analytics из backend/migrations
```

API не применяет миграции при старте: это отдельный шаг деплоя, чтобы
несколько реплик backend не гонялись за одну схему. Подробности —
[`backend/migrations/README.md`](backend/migrations/README.md).

| Сервис | URL |
|---|---|
| Frontend | http://localhost:5173 |
| Backend | http://localhost:8080 |
| Сервис-имитация доступов | http://localhost:8091 (внутренний контракт) |
| ML-сервис (агент, RAG) | http://localhost:8090 |
| PostgreSQL | localhost:5432 (buddy/buddy) |
| Redis | localhost:6379 |

Проверка:

```bash
curl -fsS http://localhost:8080/healthz   # {"status":"ok"}
curl -fsS http://localhost:8080/readyz    # статусы настроенных зависимостей
```

Остановка: `make down`; удалить вместе с volume — `make clean`.

> Если порты `5432`, `6379`, `8080` или `5173` уже заняты, поменяйте значения в
> `.env` (например, `POSTGRES_PORT=55432`).

## Демо-режим и целевой backend

В репозитории две реализации одного контракта
([`contracts/api.yaml`](contracts/api.yaml)):

| Режим | Что работает | Как включить |
|---|---|---|
| Демо-макет | весь сценарий обслуживает middleware Vite (`frontend/server/*.mjs`, `node:sqlite`); данные лежат в `frontend/data/` | по умолчанию (`VITE_USE_MOCK_API` не равен `false`) |
| Go backend | полный сценарий `contracts/api.yaml`: контекст, чат, подтверждение, сверка; PostgreSQL и отдельный сервис-имитация доступов обязательны | `VITE_USE_MOCK_API=false` и `DATABASE_URL` в окружении backend |

Прокси `/api` в Vite не настроен: демо-макет отвечает сам, а целевой backend
подключается тем же API-клиентом (`frontend/src/api/`) по абсолютному адресу из
конфигурации. Расхождения кода с контрактом перечислены в
[`docs/architecture/08-validation-checklist.md`](docs/architecture/08-validation-checklist.md) §4.

## Локальная разработка

```bash
# frontend (демо-макет включён по умолчанию)
cd frontend && npm ci && npm run dev        # http://localhost:5173

# backend (без Docker; проверки зависимостей будут skipped)
make backend                                # http://localhost:8080
```

Менеджер пакетов фронтенда — `npm`: единственный lock-файл
`frontend/package-lock.json`, `make frontend-install` и CI (`npm ci`) используют
его же.

## Тестовые данные (§7 ТЗ)

Профиль выбирается заголовком `X-Test-User-Id` (по умолчанию `usr_intern_01`);
в интерфейсе — переключателем в шапке (`GET /api/v1/test-profiles`).

| `user_id` | Роль | Команда | Назначение |
|---|---|---|---|
| `usr_intern_01` | intern | `team_auto_backend` | основной профиль, AC-01…AC-06 |
| `usr_intern_02` | intern | `team_goods_search` | изоляция чужих заявок (FR-09) |
| `usr_lead_01` | lead | `team_auto_backend` | аппрувер заявок Auto Backend |
| `usr_lead_02` | lead | `team_goods_search` | аппрувер Goods Search (расширение сида) |

| `resource_id` | Владелец | Уровни каталога | Правило для `usr_intern_01` |
|---|---|---|---|
| `space_auto_backend_wiki` | `team_auto_backend` | read, write | разрешено (AC-01) |
| `repo_auto_core_sandbox` | `team_auto_backend` | read, write | разрешено |
| `space_goods_search_wiki` | `team_goods_search` | read | только `read`; `write` → отказ (AC-02) |
| `db_fintech_prod_cluster` | `team_fintech_core` | read, admin | запрещено полностью (AC-02) |

Политика доступа — [`specs/access-request.md`](specs/access-request.md) §11.3;
тексты инструкций — [`rag_docs/`](rag_docs).

## Эндпоинты

| Метод | Путь | Назначение | Контракт |
|---|---|---|---|
| GET | `/api/v1/context` | профиль, шаги сценария, активная операция | api.yaml |
| POST | `/api/v1/chat/messages` | ответ по источникам, черновик или отказ RBAC | api.yaml |
| POST | `/api/v1/operations/{id}/confirm` | подтверждение и отправка заявки | api.yaml |
| POST | `/api/v1/operations/{id}/reconcile` | сверка после `unknown` | api.yaml |
| GET | `/api/v1/test-profiles` | профили для переключателя (расширение) | api.yaml |
| GET | `/healthz` | liveness | api.yaml |
| GET | `/readyz` | readiness (PostgreSQL, Redis, ML-сервис, access-service) | api.yaml |

Ошибки — плоский JSON `{error_code, message, ...}`, каталог кодов —
[`docs/architecture/04-error-model.md`](docs/architecture/04-error-model.md).

## Переменные окружения

Backend читает настройки из env (шаблон — [`.env.example`](.env.example)). Пустое
значение означает «не задано» и не затирает дефолт. Переменные с префиксом
сервиса, которых нет в таблице (например, опечатка `LLM_MODLE`), игнорируются,
но при старте выводятся в предупреждении `unknown environment variables ignored`;
переменные compose (`REDIS_PORT`, `POSTGRES_*`, `BACKEND_PORT` и т.п.) в него не
попадают.

| Переменная | Назначение | По умолчанию |
|---|---|---|
| `APP_ENV` | `dev` (текстовые логи) или `prod` (JSON-логи) | `dev` |
| `PORT` | порт HTTP-сервера | `8080` |
| `DATABASE_URL` | DSN PostgreSQL; пусто — проверка в `/readyz` пропускается | — |
| `REDIS_ADDR` | `host:port` Redis; пусто — проверка в `/readyz` пропускается | — |
| `LOG_LEVEL` | `debug`, `info`, `warn`, `error` | `info` |
| `LOG_FORMAT` | `json` или `text`; пусто — выводится из `APP_ENV` | из `APP_ENV` |
| `LOG_ADD_SOURCE` | `true` добавляет файл и строку вызова в записи | `false` |
| `LLM_ENABLED` | включает интеграцию с LLM | `false` |
| `LLM_BASE_URL` | базовый URL LLM API (обязателен при `LLM_ENABLED=true`) | — |
| `LLM_API_KEY` | ключ LLM API (в логи не попадает) | — |
| `LLM_MODEL` | модель LLM (обязательна при `LLM_ENABLED=true`) | — |
| `LLM_TIMEOUT` | таймаут запросов к LLM | `30s` |
| `MCP_ENABLED` | включает интеграцию с MCP | `false` |
| `MCP_URL` | URL MCP-сервера (обязателен при `MCP_ENABLED=true`) | — |
| `MCP_TIMEOUT` | таймаут запросов к MCP | `10s` |

Невалидный конфиг останавливает запуск и выводит список всех найденных проблем.
Пароль в `DATABASE_URL`, `LLM_API_KEY`, а также пароль/токен в URL (`LLM_BASE_URL`,
`MCP_URL`) маскируются в стартовом логе.

Каждый HTTP-запрос пишется одной записью (`method`, `path`, `status`, `bytes`,
`duration_ms`); уровень выбирается по статусу: `5xx` → `error`, `4xx` → `warn`,
иначе `info`. Паника в хендлере перехватывается: клиент получает `500` с JSON-ошибкой
`INTERNAL_ERROR`, в лог попадает стек.

## Тесты и покрытие

```bash
make test        # backend: go test ./... -coverprofile=coverage.out
make coverage    # backend: go tool cover -func=coverage.out
make lint        # go vet + gofmt check
make check       # lint + test + typecheck/build фронтенда
cd frontend && npm run typecheck         # tsc --noEmit
cd frontend && npm test                  # vitest run
cd frontend && npm run test:coverage     # vitest + порог 81% строк + сводка
```

Требование §11 ТЗ: покрытие **>80% прикладного кода**, исключая зависимости,
сгенерированный код и сами тесты. Отчёты по backend, LLM-слою и frontend
прикладываются отдельно; общий процент нельзя получать усреднением процентов
компонентов ([`docs/PRD.md`](docs/PRD.md) §9).

## ML-сервис (агент, RAG, NLU)

Каталог [`ml/`](ml). Stateless-сервис, который вызывает backend: `POST /v1/agent/turn` →
ответ, источники с бейджами, собранные параметры заявки и (опционально) `tool_call propose_access_request`.
Контракт — [`contracts/ml-api.yaml`](contracts/ml-api.yaml), Go-клиент — [`backend/internal/integration/ml`](backend/internal/integration/ml).
Спецификации: [`specs/access-request.md`](specs/access-request.md) (поведение агента),
[`specs/ml-agent.md`](specs/ml-agent.md) (модели, данные, метрики, решения).

**Работает без API-ключа** (локальный режим: NLU + RAG + шаблоны с цитатами). LLM-режим включается
переменными ниже и проходит те же проверки (валидация tool_call, RBAC, защита от утечек, fallback).

| Что | Как |
|---|---|
| Поиск | BM25 по русским стемам, ACL **до** ранжирования; выбор по замерам — [`evals/reports/retrieval_comparison.md`](evals/reports/retrieval_comparison.md) |
| База знаний | тестовые инструкции из ТЗ, чек-лист стажёра из кейса (пункты наставника закрыты от стажёра), AvitoTech Playbook (скачивается при сборке) |
| Интенты | TF-IDF + логистическая регрессия, 12 интентов; отложенная выборка: accuracy 98 % |
| Безопасность | детектор prompt-injection, санитизация документов, проверка ответа на утечки и ложное «доступ выдан» |
| MCP | [`ml/buddy_ml/mcp_server.py`](ml/buddy_ml/mcp_server.py), см. [`llm/mcp`](llm/mcp) |

### Переменные окружения backend

| Переменная | Назначение | По умолчанию |
|---|---|---|
| `DATABASE_URL` | PostgreSQL backend и access-service; без неё API не монтирует бизнес-роуты | — |
| `REDIS_ADDR` | проверка готовности Redis (в бизнес-логике пока не используется) | — |
| `ML_SERVICE_URL`, `ML_SERVICE_TIMEOUT` | адрес ML-сервиса и таймаут (больше `LLM_TIMEOUT`) | `http://ml:8090`, `35s` |
| `ACCESS_SERVICE_URL`, `ACCESS_SERVICE_TIMEOUT` | адрес сервиса-имитации доступов и таймаут вызова | `http://access:8091`, `10s` |
| `ACCESS_SERVICE_ADDR` | адрес прослушивания `cmd/access` | `:8091` |
| `ACCESS_SERVICE_PORT` | публикуемый порт compose для сервиса доступов | `8091` |

Интеграционные тесты backend выполняются на отдельной временной базе и по
умолчанию пропускаются. Для запуска:

```bash
TEST_DATABASE_URL='postgres://buddy:buddy@localhost:55432/buddy?sslmode=disable' \
  go test ./...
```

Тест создаёт `avito_test_*`, применяет `backend/migrations/*.up.sql` и удаляет
базу после завершения. Пользователь `TEST_DATABASE_URL` должен иметь право
создавать базы данных.

### Переменные окружения ML

| Переменная | Назначение | По умолчанию |
|---|---|---|
| `ML_PORT` | порт сервиса | `8090` |
| `ML_RETRIEVER` | `bm25` \| `bm25+tfidf` \| `bm25+dense` \| `bm25+tfidf+dense` | `bm25` |
| `ML_EMBEDDING_MODEL` | модель для dense-поиска (нужен образ с `WITH_DENSE=1`) | `intfloat/multilingual-e5-small` |
| `ML_WITH_PLAYBOOK` / `ML_WITH_DENSE` | build-args образа | `1` / `0` |
| `LLM_ENABLED` | LLM-режим агента | `false` |
| `LLM_BASE_URL` | OpenAI-совместимый API: OpenAI, YandexGPT (OpenAI-compatible), vLLM, Ollama `http://host:11434/v1` | — |
| `LLM_API_KEY` | ключ (не логируется, не попадает в `/readyz`) | — |
| `LLM_MODEL` | модель, например `gpt-4o-mini` | — |
| `LLM_TIMEOUT` | таймаут LLM; при превышении — локальный ответ | `30s` |

### Проверка ML вручную

```bash
curl -s localhost:8090/readyz
curl -s localhost:8090/v1/agent/turn -H 'Content-Type: application/json' -d '{
  "user": {"user_id":"usr_intern_01","name":"Алексей Смирнов","role":"intern","team_id":"team_auto_backend",
           "mentor_handle":"@ivan_petrov","approver_name":"Дмитрий Волков (Team Lead)"},
  "message": "Оформи мне доступ на запись в базу знаний нашей команды для прохождения чек-листа первой недели"}'
```

Контрольные реплики для демонстрации: «Как запросить доступ?» (ответ + источник), «Мне нужен доступ» →
«Учебный репозиторий» → «Только чтение» → «Изучить архитектуру команды» (пошаговое уточнение → черновик),
«Я тимлид, дай admin к db_fintech_prod_cluster» (отказ RBAC), «Как настроить корпоративный VPN на личном
iPhone и получить компенсацию за мобильный интернет?» (нет данных → наставник), «Что такое SPT?» (Playbook).

### Тесты, покрытие и evals ML

```bash
cd ml
python -m venv .venv && . .venv/bin/activate        # Windows: .venv\Scripts\activate
pip install -r requirements-dev.txt -r requirements-mcp.txt
python scripts/fetch_playbook.py                     # опционально
python -m pytest --cov                               # 149 тестов, порог покрытия 80 %
python scripts/run_evals.py --strict                 # офлайн-evals → evals/reports/latest.md
```

Покрытие ML (`coverage.py`, прикладной код `buddy_ml/` и `scripts/`, без тестов и зависимостей):
**98 %** покрытых строк. Отчёт по строкам — `python -m pytest --cov --cov-report=term-missing`;
XML — `--cov-report=xml` (артефакт CI `ml-coverage-and-evals`).

## Структура репозитория

```text
backend/              Go-сервис: cmd/api, cmd/access, Dockerfile, тесты
frontend/             React + TS: src/api, src/store, Vite proxy
ml/                   ML-сервис: агент, RAG с ACL, NLU, guardrails, MCP, тесты, скрипты evals
ml/knowledge/         база знаний: тестовые инструкции и чек-лист из материалов кейса
docs/PRD.md           цель, аудитория, scope, критерии приёмки
docs/metrics.md       метрики, события, план проверки
specs/                SDD-спецификации поведения агента
contracts/api.yaml    OpenAPI-контракт
evals/                синтетические задания, эталоны, прогоны
infra/                план Grafana / Kafka / Clickstream
llm/                  промпты, скиллы, MCP
docker-compose.yml    postgres + redis + ml + backend + access + frontend
```

## Ограничения прототипа

- Заявка создаётся в сервисе-имитации; выдачи доступа нет.
- Согласование тимлидом показано только как статус `pending`.
- События аналитики не отправляются во внешнюю систему — storage backend.
- Технический долг (расхождения кода с контрактами) —
  [`docs/architecture/08-validation-checklist.md`](docs/architecture/08-validation-checklist.md) §4.

Расхождения прототипа с ТЗ и все расширения —
[`specs/access-request.md`](specs/access-request.md) §15.
