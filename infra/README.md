# Инфраструктура

Сейчас в [`docker-compose.yml`](../docker-compose.yml) поднимаются PostgreSQL,
Redis, backend и frontend.

## Совместимость со стеком Авито

| Компонент | Статус | План |
|---|---|---|
| Docker | ✅ используется | compose для локальной разработки и CI |
| CI/CD | ✅ базовый workflow | `.github/workflows/ci.yml`: lint, тесты, сборка образов |
| Grafana | 🕓 запланировано | дашборды: статусы заявок, p95 API, ошибки инструментов |
| Prometheus | 🕓 запланировано | `/metrics` в backend, scrape-конфиг |
| Kafka / RPC | 🕓 запланировано | события `access.request.*` из `docs/metrics.md` |
| Clickstream | 🕓 запланировано | продуктовые события UI (начало диалога, отправка заявки) |

## Правила

- Новые сервисы добавляются только с healthcheck и `depends_on: condition: service_healthy`.
- Секреты — через переменные окружения (`.env`, в git не попадает).
- Статический фронтенд взаимодействует с backend только через явный API-клиент
  (`frontend/src/api/`).
