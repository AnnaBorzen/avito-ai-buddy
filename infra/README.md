# Инфраструктура

Сейчас в [`docker-compose.yml`](../docker-compose.yml) поднимаются PostgreSQL 17,
Redis 7.4, backend и frontend.

## Совместимость со стеком Авито

| Компонент | Статус | План |
|---|---|---|
| Docker | ✅ используется | compose для локальной разработки и CI |
| CI/CD | ✅ базовый workflow | `.github/workflows/ci.yml`: lint, тесты, сборка образов |
| Grafana | 🕓 запланировано | дашборды: операции по статусам, p95 API, ошибки upstream |
| Prometheus | 🕓 запланировано | `/metrics` в backend, scrape-конфиг |
| Kafka / RPC | 🕓 запланировано | доставка событий из [`docs/metrics.md`](../docs/metrics.md) (§2, 9 событий) |
| Clickstream | 🕓 запланировано | те же события продукта; источник — только backend |

## Правила

- Новые сервисы добавляются только с healthcheck и `depends_on: condition: service_healthy`.
- Секреты — через переменные окружения (`.env`, в git не попадает).
- Фронтенд общается с backend только через явный API-клиент (`frontend/src/api/`)
  и только по контракту [`contracts/api.yaml`](../contracts/api.yaml).
- Внутренние вызовы сервиса-имитации (`/internal/*`) не публикуются наружу.
