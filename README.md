# Avito AI Buddy

AI-бадди для сотрудников: помогает оформить заявку на доступ к внутренней системе
и довести её до ответственного. Сейчас в репозитории — рабочий каркас:
связка **frontend → backend → PostgreSQL/Redis** в Docker.

## Стек

| Слой | Технологии |
|---|---|
| Backend | Go 1.26, `net/http`, `log/slog`, `koanf` (env-конфиг) |
| Frontend | React 19, TypeScript, Vite, TanStack Query, Zustand, Zod |
| ML-сервис | Python 3.12, Flask + gunicorn, scikit-learn, snowball-стемминг; опц. sentence-transformers, MCP SDK |
| Данные | PostgreSQL 17, Redis 7.4 |
| Инфраструктура | Docker, Docker Compose, GitHub Actions |

## Быстрый старт (Docker)

```bash
cp .env.example .env      # при необходимости поменяйте порты и пароли
make up                   # docker compose up -d --build
```

Сервисы после старта:

| Сервис | URL |
|---|---|
| Frontend | http://localhost:5173 |
| Backend | http://localhost:8080 |
| ML-сервис (агент, RAG) | http://localhost:8090 |
| PostgreSQL | localhost:5432 (buddy/buddy) |
| Redis | localhost:6379 |

Проверка:

```bash
curl -fsS http://localhost:8080/healthz   # {"status":"ok"}
curl -fsS http://localhost:8080/readyz    # статусы postgres и redis
```

Остановка:

```bash
make down     # остановить
make clean    # остановить и удалить volumes
```

> Если порты `5432`, `6379`, `8080` или `5173` уже заняты локальными сервисами,
> поменяйте значения в `.env` (например, `POSTGRES_PORT=55432`) — compose
> подставит их при публикации портов.

## Локальная разработка

```bash
# backend (без Docker; проверки зависимостей будут skipped)
make backend                     # http://localhost:8080

# frontend
make frontend-install            # npm ci
make frontend                    # http://localhost:5173, проксирует /healthz, /readyz, /api на :8080
```

`make backend` подхватывает переменные из `.env` в корне репозитория (если файл есть).
Раскомментируйте в нём `DATABASE_URL` и `REDIS_ADDR`, чтобы `/readyz` проверял
локальные PostgreSQL и Redis, а не пропускал проверки.

## Переменные окружения

Backend читает настройки из env (шаблон — [`.env.example`](.env.example)).
Пустое значение означает «не задано» и не затирает дефолт.
Переменные с префиксом сервиса, которых нет в таблице (например, опечатка
`LLM_MODLE`), игнорируются, но при старте выводятся в предупреждении
`unknown environment variables ignored`; переменные compose (`REDIS_PORT`,
`POSTGRES_*`, `BACKEND_PORT` и т.п.) в него не попадают.

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
Пароль в `DATABASE_URL` и `LLM_API_KEY`, а также пароль/токен в URL
(`LLM_BASE_URL`, `MCP_URL`) маскируются в стартовом логе.

Каждый HTTP-запрос пишется одной записью (`method`, `path`, `status`, `bytes`,
`duration_ms`), уровень выбирается по статусу: `5xx` → `error`, `4xx` → `warn`,
иначе `info`. Паника в хендлере перехватывается: клиент получает `500` с JSON-ошибкой
`INTERNAL_ERROR`, в лог попадает стек. Стандартный `log` и `http.Server.ErrorLog`
перенаправлены в slog, поэтому формат логов остаётся единым.

## Эндпоинты

| Метод | Путь | Назначение |
|---|---|---|
| GET | `/healthz` | liveness: процесс жив |
| GET | `/readyz` | readiness: TCP-доступность PostgreSQL и Redis, при ошибке — `503` |

Контракт: [`contracts/api.yaml`](contracts/api.yaml).

## Тесты и покрытие

```bash
make test       # go test ./... -coverprofile=coverage.out
make coverage   # go tool cover -func=coverage.out
make lint       # go vet + gofmt check
make check      # lint + test + typecheck/build фронтенда
```

Отчёт покрытия — `backend/coverage.out` (в git не попадает).

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
| `ML_SERVICE_URL`, `ML_TIMEOUT` | для backend: адрес ML и таймаут (больше `LLM_TIMEOUT`) | `http://ml:8090`, `35s` |

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
backend/              Go-сервис: cmd/api, Dockerfile, тесты
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
docker-compose.yml    postgres + redis + ml + backend + frontend
```
