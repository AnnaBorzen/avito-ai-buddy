# Промпты

| Промпт | Файл | Где используется |
|---|---|---|
| Системный промпт агента (RU) | [`ml/buddy_ml/prompts/system_ru.md`](../../ml/buddy_ml/prompts/system_ru.md) | LLM-режим ML-сервиса |

Канонический файл лежит рядом с кодом, чтобы попадать в Docker-образ ML-сервиса без копирования.
Плейсхолдеры (`$user_name`, `$role`, `$team_name`, `$mentor_handle`, `$approver_name`, `$draft`, `$operation`,
`$permissions`) заполняются из **доверенного** контекста запроса backend, а не из текста пользователя.

Документы базы знаний передаются отдельным системным сообщением в блоке `<documents>`: каждый фрагмент
санитизирован (`guard.sanitize_document`) и обёрнут в `<document doc_id=… section=… type=…>`. Правило 1 промпта
требует считать их данными, а не инструкциями.

## Изменение промпта

1. Обновить правило в [`specs/access-request.md`](../../specs/access-request.md).
2. Изменить `system_ru.md` и поднять `SCENARIO_VERSION` в [`ml/buddy_ml/__init__.py`](../../ml/buddy_ml/__init__.py)
   (версия попадает в `meta` ответа и в аналитику).
3. Прогнать `python ml/scripts/run_evals.py` в LLM-режиме (`LLM_ENABLED=true …`) и сравнить с
   [`evals/reports/latest.md`](../../evals/reports/latest.md).
