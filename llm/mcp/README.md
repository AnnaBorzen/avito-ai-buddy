# MCP-сервер инструментов AI-buddy

Реализация: [`ml/buddy_ml/mcp_server.py`](../../ml/buddy_ml/mcp_server.py) (официальный Python SDK `mcp` 2.x,
транспорт stdio). Позволяет подключить те же инструменты к платформе ИИ-агентов Авито, не дублируя логику.

| Инструмент | Что делает | Меняет состояние |
|---|---|---|
| `search_knowledge(query, k)` | поиск по базе знаний среди документов, доступных пользователю | нет |
| `list_resources()` | каталог ресурсов и уровни, которые пользователь может запросить | нет |
| `check_access_policy(resource_id, access_level)` | консультативная RBAC-проверка | нет |
| `propose_access_request(resource_id, access_level, justification)` | валидирует параметры и возвращает черновик для карточки | нет |

Инструмента отправки заявки **нет**: отправка выполняется backend'ом только по подтверждению в UI (FR-05).

## Доверенный контекст

Параметров `user_id`/`team_id`/`role` у инструментов нет. Платформа запускает процесс сервера на сессию
пользователя и передаёт контекст из авторизации через окружение:

- `BUDDY_USER_CONTEXT` — JSON `{"user_id","role","team_id","name","team_name","mentor_handle","approver_name"}`;
- `BUDDY_USER_ID` — для демо: один из синтетических профилей (`usr_intern_01`, `usr_intern_02`, `usr_lead_01`).

## Запуск

```bash
pip install -r ml/requirements-mcp.txt
cd ml && BUDDY_USER_ID=usr_intern_01 python -m buddy_ml.mcp_server
```

Пример подключения в MCP-клиенте — [`mcp.example.json`](mcp.example.json).
