# Avito AI Buddy

AI-бадди для сотрудников: помогает оформить заявку на доступ к внутренней системе
и довести её до ответственного. Сейчас в репозитории — рабочий каркас:
связка **frontend → backend → PostgreSQL/Redis** в Docker.

## Стек

| Слой | Технологии |
|---|---|
| Backend | Go 1.26, стандартная библиотека (`net/http`, `log/slog`) |
| Frontend | React 19, TypeScript, Vite, TanStack Query, Zustand, Zod |
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

## Структура репозитория

```text
backend/              Go-сервис: cmd/api, Dockerfile, тесты
frontend/             React + TS: src/api, src/store, Vite proxy
docs/PRD.md           цель, аудитория, scope, критерии приёмки
docs/metrics.md       метрики, события, план проверки
specs/                SDD-спецификации поведения агента
contracts/api.yaml    OpenAPI-контракт
evals/                синтетические задания, эталоны, прогоны
infra/                план Grafana / Kafka / Clickstream
llm/                  промпты, скиллы, MCP
docker-compose.yml    postgres + redis + backend + frontend
```
