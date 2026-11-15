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
| Frontend | React 18, TypeScript, Vite 5, Tailwind |
| Тесты фронтенда | Vitest + Testing Library (порог покрытия 81% строк) |
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
| PostgreSQL | localhost:5432 (buddy/buddy) |
| Redis | localhost:6379 |

Проверка:

```bash
curl -fsS http://localhost:8080/healthz   # {"status":"ok"}
curl -fsS http://localhost:8080/readyz    # статусы postgres и redis
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
| Go backend | пока только `GET /healthz`, `GET /readyz`; бизнес-эндпоинты в разработке | `VITE_USE_MOCK_API=false`, backend на `:8080` |

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
| GET | `/readyz` | readiness (PostgreSQL, Redis) | api.yaml |

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
cd frontend && npm test                  # vitest run
cd frontend && npm run test:coverage     # vitest + порог 81% строк + сводка
```

Требование §11 ТЗ: покрытие **>80% прикладного кода**, исключая зависимости,
сгенерированный код и сами тесты. Отчёты по backend, LLM-слою и frontend
прикладываются отдельно; общий процент нельзя получать усреднением процентов
компонентов ([`docs/PRD.md`](docs/PRD.md) §9).

## Структура репозитория

```text
docs/tz/              эталон ТЗ: снимок docx + правила сверки
docs/PRD.md           цель, состав MVP, FR, интерфейс, DoD
docs/metrics.md       метрики, 9 событий, общие поля
docs/architecture.md  индекс архитектуры + docs/architecture/01…08
specs/                SDD-спецификация поведения
contracts/            api.yaml, mcp-tools.yaml, access-service.openapi.yaml
rag_docs/             документы корпуса (описание — docs/rag-corpus.md)
evals/                задания, эталоны, прогоны
llm/                  промпты, скиллы, MCP (см. llm/README.md)
infra/                план Grafana / Kafka / Clickstream
scripts/              tz_snapshot.py — генерация снимка ТЗ
backend/              Go-сервис: cmd/api, transport/http, Dockerfile
frontend/             React + TS: src/, server/ (демо-API), tests/
docker-compose.yml    postgres + redis + backend + frontend
```

## Ограничения прототипа

- Заявка создаётся в сервисе-имитации; выдачи доступа нет.
- Согласование тимлидом показано только как статус `pending`.
- События аналитики не отправляются во внешнюю систему — storage backend.
- Технический долг (расхождения кода с контрактами) —
  [`docs/architecture/08-validation-checklist.md`](docs/architecture/08-validation-checklist.md) §4.

Расхождения прототипа с ТЗ и все расширения —
[`specs/access-request.md`](specs/access-request.md) §15.
