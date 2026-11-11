# Техническая архитектура AI-buddy

**Статус:** Proposed  
**Версия:** 1.0

| Параметр | Значение |
|---|---|
| Backend | Go |
| Database | PostgreSQL |
| Frontend | React + TypeScript |
| Architecture Style | Modular Monolith |
| API | REST |
| AI Integration | LLM + MCP |
| Deployment | Docker |

---

## 1. Выбранный подход

Для MVP используется архитектура **Modular Monolith** (модульный монолит).

Все backend-компоненты работают в рамках одного Go-приложения, но разделены на логические модули с чёткой ответственностью. Каждый модуль отвечает за определённую часть бизнес-логики и взаимодействует с другими модулями через интерфейсы.

---

## 2. Основные технологические решения

| Компонент | Технология | Назначение |
|---|---|---|
| Backend | Go | API и бизнес-логика |
| HTTP Router | Chi / Gin | Обработка HTTP-запросов |
| Database | PostgreSQL | Постоянное хранение данных |
| Database Access | pgx | Работа с PostgreSQL |
| Migrations | golang-migrate | Управление схемой БД |
| Validation | go-playground/validator | Валидация входных данных |
| AI | LLM API | Генерация ответов |
| AI Tools | MCP | Вызов инструментов |
| Frontend | React + TypeScript | Пользовательский интерфейс |
| Containerization | Docker | Запуск приложения |
| Logging | slog | Структурированные логи |

> **TODO:** добавить Redis позднее.

---

## 3. Описание директорий

### 3.1 Назначение слоёв

| Директория | Назначение |
|---|---|
| `internal/domain` | Основные бизнес-сущности и правила предметной области |
| `internal/service` | Бизнес-логика |
| `internal/repository` | Взаимодействие с PostgreSQL |
| `internal/integration` | Адаптеры для внешних систем |

### 3.2 Структура проекта

```text
backend/
│
├── cmd/
│   └── api/
│       └── main.go
│
├── internal/
│   │
│   ├── app/
│   │   └── app.go
│   │
│   ├── config/
│   │   └── config.go
│   │
│   ├── transport/
│   │   └── http/
│   │       ├── router.go
│   │       ├── middleware/
│   │       ├── handlers/
│   │       ├── dto/
│   │       └── errors.go
│   │
│   ├── domain/
│   │   ├── user/
│   │   ├── team/
│   │   ├── resource/
│   │   ├── operation/
│   │   ├── ticket/
│   │   ├── conversation/
│   │   └── scenario/
│   │
│   ├── service/
│   │   ├── context/
│   │   ├── chat/
│   │   ├── operations/
│   │   ├── rbac/
│   │   ├── knowledge/
│   │   └── analytics/
│   │
│   ├── integration/
│   │   ├── llm/
│   │   ├── mcp/
│   │   └── access/
│   │       └── mock/
│   │
│   ├── repository/
│   │   └── postgres/
│   │
│   └── infrastructure/
│       ├── database/
│       └── logger/
│
├── migrations/
└── tests/
```

---

## 4. База данных

| Таблица | Назначение |
|---|---|
| `users` | Тестовые пользователи |
| `teams` | Команды |
| `resources` | Каталог ресурсов |
| `resource_permissions` | Матрица прав |
| `scenarios` | Сценарии адаптации |
| `scenario_steps` | Этапы сценария |
| `task_instances` | Экземпляры задач |
| `conversations` | Диалоги |
| `messages` | Сообщения |
| `operations` | Операции |
| `tickets` | Созданные заявки |
| `knowledge_documents` | Метаданные документов |
| `analytics_events` | Аналитические события |

---

## 5. Обработка ошибок

Backend должен различать следующие категории ошибок:

| Ошибка | HTTP | Поведение |
|---|---|---|
| Некорректный запрос | 400 | Вернуть описание ошибки |
| Не авторизован | 401 | Запросить авторизацию |
| Нет прав | 403 | Отказать в операции |
| Операция не найдена | 404 | Вернуть ошибку |
| Устаревшая версия черновика | 409 | Потребовать обновить карточку |
| Повторная операция | 409 или сохранённый результат | Не создавать дубликат |
| Ошибка LLM | 502 | Предложить повторить запрос |
| Таймаут внешнего сервиса | 504 | Перевести операцию в `unknown` |
| Внутренняя ошибка | 500 | Записать технический лог |

### 5.1 Формат ответа с ошибкой

```json
{
  "error": {
    "code": "RBAC_ACCESS_DENIED",
    "message": "Недостаточно прав для выполнения операции.",
    "request_id": "req-123"
  }
}
```