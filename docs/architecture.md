# Архитектура AI-buddy: индекс решений

| Параметр | Значение |
|---|---|
| Версия | 1.0 |
| Источник требований | `Авито_ТЗ.docx` §1–§11 ([снимок](tz/avito-tz.md), [карта](tz/README.md)) |
| Спецификация поведения | [`../specs/access-request.md`](../specs/access-request.md) |
| Контракты | [`../contracts/api.yaml`](../contracts/api.yaml), [`../contracts/mcp-tools.yaml`](../contracts/mcp-tools.yaml), [`../contracts/access-service.openapi.yaml`](../contracts/access-service.openapi.yaml) |

Документы по темам:

| Документ | О чём |
|---|---|
| [`architecture/01-c4-diagrams.md`](architecture/01-c4-diagrams.md) | C4: контекст, контейнеры, компоненты backend |
| [`architecture/02-services.md`](architecture/02-services.md) | границы сервисов и что имитируем |
| [`architecture/03-entities.md`](architecture/03-entities.md) | сущности, ID, состояния, хранение |
| [`architecture/04-error-model.md`](architecture/04-error-model.md) | единый формат ошибок и политика повторов |
| [`architecture/05-auth.md`](architecture/05-auth.md) | идентичность, тестовые профили, RBAC |
| [`architecture/06-versioning.md`](architecture/06-versioning.md) | версионирование API и контракт-первый |
| [`architecture/07-sequences.md`](architecture/07-sequences.md) | последовательности AC-01…AC-04 |
| [`architecture/08-validation-checklist.md`](architecture/08-validation-checklist.md) | чек-лист проверки контрактов и документов |

## 1. Ключевые решения

| ID | Решение | Почему | Где закреплено |
|---|---|---|---|
| AD-1 | Один экран, один сценарий `scn_team_space_access` | §1 и §4 ТЗ: MVP — глубокая проработка одного сценария | PRD §1, §7 |
| AD-2 | Три независимых словаря статусов (шаг / операция / заявка) | §4 ТЗ; смешение рождает ложное «доступ выдан» | спека §2 |
| AD-3 | Все решения о правах и валидации принимает backend, не LLM | §5, §8.1 ТЗ; защита от prompt injection | спека §5.1, §11.3 |
| AD-4 | Каталог ресурса приоритетнее общего enum уровня | §7.2 против §8.1; даёт корректный AC-02 | спека §11.3 |
| AD-5 | `unknown` — отдельное состояние, выход только через `reconcile` | AC-04: слепой повтор создаёт дубли | спека §3.1, §13.3 |
| AD-6 | Отправка заявки — не инструмент модели, а backend по подтверждению | §8 ТЗ: два контура | спека §5, mcp-tools |
| AD-7 | Ошибки — плоский JSON (`error_code`, `message`) | §10.3 ТЗ задаёт такую форму | 04-error-model |
| AD-8 | Идентичность — `X-Test-User-Id` с сидовыми профилями | §10 ТЗ; в проде — SSO на той же границе | 05-auth |
| AD-9 | Контракт-первый: изменение API начинается с `contracts/` | §5 ТЗ (SDD) | 06-versioning |
| AD-10 | Один контракт — две реализации (демо-макет и Go backend) | у команды уже есть демо-макет для защиты; целевая реализация — Go | 02-services §4 |

## 2. Состояние реализации

Документы описывают **целевую** архитектуру прототипа. Фактическое состояние кода:

| Компонент | Что есть сегодня |
|---|---|
| `frontend/` | React 18 + TypeScript + Vite, Vitest, Tailwind; демо-API смонтирован middleware'ом Vite (`frontend/server/*.mjs`, `node:sqlite`) и повторяет часть `contracts/api.yaml` |
| `backend/` | Go-каркас: конфиг (koanf), логгер (slog), `GET /healthz`, `GET /readyz`; доменные пакеты пусты |
| `docker-compose.yml` | PostgreSQL 17, Redis 7.4, backend, frontend |
| `contracts/` | три контракта — источник истины для обеих реализаций |

Расхождения кода с контрактами чинятся кодом, а не документами, и перечислены в
одном месте — [`08-validation-checklist.md`](architecture/08-validation-checklist.md) §4.

## 3. Порядок чтения

1. [`PRD.md`](PRD.md) — что и зачем.
2. [`../specs/access-request.md`](../specs/access-request.md) — как ведёт себя система.
3. `contracts/*` — в каком формате.
4. `docs/architecture/01…08` — где это живёт и как проверяется.
