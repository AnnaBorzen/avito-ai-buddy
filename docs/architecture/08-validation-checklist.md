# Чек-лист валидации контрактов и документов

Проверяется перед сдачей любого изменения `contracts/`, `specs/`, `docs/`,
`rag_docs/`.

## 1. YAML и ссылки

- [ ] `contracts/api.yaml`, `contracts/ml-api.yaml`, `contracts/mcp-tools.yaml`,
      `contracts/access-service.openapi.yaml` разбираются любым YAML 1.2-парсером.
- [ ] Все `$ref: "#/..."` разрешаются внутри своего файла (проверка скриптом).
- [ ] `info.version` поднят, если изменился контракт (см. `06-versioning.md`).

## 2. Значения из ТЗ перенесены дословно

| Что | Где | Ожидаемое значение |
|---|---|---|
| `resource_id` | api.yaml, mcp-tools.yaml, access-service | 4 значения из §7.2 |
| `StepStatus` | api.yaml | `not_started`, `in_progress`, `blocked`, `done` |
| `OperationStatus` | api.yaml | `draft`, `awaiting_confirmation`, `submitting`, `succeeded`, `failed`, `unknown` |
| `TicketStatus` | api.yaml, access-service | `pending`, `approved`, `rejected` |
| `access_level` инструмента | mcp-tools.yaml | `read`, `write` |
| `justification` | mcp-tools.yaml | `minLength 10`, `maxLength 300` |
| `simulate_mode` | api.yaml | `normal`, `timeout`, `service_error` |
| события | docs/metrics.md | 9 имён из §6 |
| `user_id` профилей | api.yaml `TestProfile`, спека §11.1 | `usr_intern_01`, `usr_intern_02`, `usr_lead_01`, `usr_lead_02` |
| заголовки | api.yaml | `X-Test-User-Id`, `Idempotency-Key` |

## 3. Согласованность документов

- [ ] Каждый FR-01…FR-12 имеет строку трассировки (спека §10) и проверку.
- [ ] Каждый AC-01…AC-06 описан в спеке §12 и в sequence-диаграммах 07.
- [ ] Политика доступа описана ровно один раз (спека §11.3) и на неё ссылаются
      api.yaml, 05-auth и mcp-tools.
- [ ] Каждый пункт расширения из `x-extensions` (api.yaml) есть в спеке §15 и
      наоборот.
- [ ] Тексты статусов не обещают выданный доступ (INV-05).
- [ ] В документах нет незакрытых заглушек (проверка — §5).

## 4. Известные расхождения кода (нужна правка кода, не документов)

| # | Расхождение | Где | Чем закрывается |
|---|---|---|---|
| 1 | Демо-макет пишет события с полями `event_name`, `time`, `version` вместо `event`, `occurred_at`, `scenario_version` | `frontend/server/service.mjs` | приведение к `docs/metrics.md` §3 |
| 2 | Демо-макет API (`frontend/server/*.mjs`) включён по умолчанию и повторяет контракт вручную; Go backend реализует тот же контракт, но frontend не проксирует на него запросы | `frontend/vite.config.ts`, `frontend/server/*.mjs` | сверка макета с `contracts/api.yaml`; для сквозной проверки backend — прокси `/api/v1` на `backend:8080` |

Расхождения фиксируются здесь, а не раскидываются по PRD: это единый список
технического долга прототипа.

## 5. Проверки перед сдачей

```bash
# YAML + разрешимость $ref
python3 - << 'PY'
import glob, yaml
for f in glob.glob('contracts/*.yaml'):
    yaml.safe_load(open(f)); print('yaml ok:', f)
PY

# отсутствие заглушек (шаблон собран из частей, чтобы файл не находил сам себя)
grep -rnE 'TO(DO)|T(BD)|FIX(ME)' docs specs contracts rag_docs README.md || echo 'no placeholders'

# дословность снимка ТЗ
md5sum ../../../Авито_ТЗ.docx   # должно совпасть с docs/tz/README.md
```

## 6. Ревью-вопросы

- Не появился ли второй владелец правила (копия политики, копия текста статуса)?
- Не выведен ли бизнес-смысл из названия/текста вместо контракта?
- Сохранена ли семантика «нет поля ≠ null ≠ пусто» в новых схемах?
- Ограничено ли действие областью своих данных (не перезаписывает ли снимок
  чужие изменения)?
