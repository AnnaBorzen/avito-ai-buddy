"""Конвертирует чек-лист стажёра Авито (материал кейса, xlsx) в документы базы знаний.

Источник: https://docs.google.com/spreadsheets/d/1qKFqOzfSZ2jEz57jBSmq7rukDXi_tRfWohgemViceV0
(ссылка из PDF кейса Hack&Change 2026, раздел «Источники информации»).

Использование:
    python ml/scripts/build_checklist_docs.py path/to/checklist.xlsx ml/knowledge/case_materials

Пункты, адресованные наставнику (бадди), выносятся в отдельный документ с
allowed_roles = mentor/lead — так retrieval стажёра их не видит (демонстрация ACL до LLM).
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

SOURCE_URL = (
    "https://docs.google.com/spreadsheets/d/1qKFqOzfSZ2jEz57jBSmq7rukDXi_tRfWohgemViceV0"
)

# Начала пунктов листа «Материалы для всех», которые адресованы наставнику.
MENTOR_PREFIXES = (
    "Добавь стажёра",
    "Запроси у куратора стажировки методичку наставника",
    "На 1:1 обсуди со стажёром",
    "Сходи на бриф для бадди",
)

PERIOD_ORDER = {
    "1 неделя": 1,
    "2 неделя": 2,
    "1 месяц": 3,
    "2 месяц": 4,
    "3 месяц": 5,
    "После 3-х месяцев": 6,
    "после 3-х месяцев": 6,
}


def _clean(text: str) -> str:
    text = str(text).replace("\r", "").strip()
    text = re.sub(r"[ \t]+", " ", text)
    return "\n".join(line.strip() for line in text.split("\n"))


def _rows(ws):
    for row in ws.iter_rows(values_only=True, min_row=2):
        vals = [_clean(v) for v in row if v not in (None, "")]
        if vals:
            yield vals


def _item(vals: list[str]) -> dict:
    period = vals[-1]
    body = vals[:-1]
    title = body[0].split("\n")[0].strip().rstrip(":")
    details = "\n".join(body[1:]) if len(body) > 1 else "\n".join(body[0].split("\n")[1:])
    return {"title": title, "details": details.strip(), "period": period, "raw": body}


def _section(item: dict) -> str:
    text = f"## {item['period']}: {item['title']}\n"
    text += f"Период прохождения: {item['period']}.\n"
    if item["details"]:
        text += item["details"] + "\n"
    return text


def _doc(doc_id: str, title: str, roles: list[str], intro: str, items: list[dict]) -> str:
    items = sorted(items, key=lambda i: PERIOD_ORDER.get(i["period"], 99))
    head = (
        "---\n"
        f"doc_id: {doc_id}\n"
        f'title: "{title}"\n'
        f"allowed_roles: {roles!r}\n".replace("'", '"')
        + 'allowed_teams: ["*"]\n'
        "is_synthetic: false\n"
        "source_type: case_material\n"
        f"source_url: {SOURCE_URL}\n"
        "---\n"
    )
    return head + intro + "\n\n" + "\n".join(_section(i) for i in items)


def build(xlsx: Path, out_dir: Path) -> list[Path]:
    import openpyxl  # локальная зависимость скрипта сборки, не сервиса

    wb = openpyxl.load_workbook(xlsx, data_only=True)
    general, backend = wb.worksheets[0], wb.worksheets[1]
    intern_items, mentor_items, seen = [], [], set()
    for vals in _rows(general):
        item = _item(vals)
        text = " ".join(item["raw"])
        key = (item["title"], item["period"])
        is_mentor = any(text.startswith(p) or p in text[:80] for p in MENTOR_PREFIXES)
        if key in seen and not is_mentor:
            # дубль «Пройди опрос…» во второй раз относится к наставнику
            is_mentor = True
        seen.add(key)
        (mentor_items if is_mentor else intern_items).append(item)
    backend_items = [_item(v) for v in _rows(backend)]

    disclaimer = (
        "Источник: чек-лист стажёра, предоставленный Авито в материалах кейса хакатона. "
        "Используется как справочный материал по этапам адаптации; конкретные процедуры "
        "доступа в прототипе описаны в тестовых инструкциях."
    )
    out_dir.mkdir(parents=True, exist_ok=True)
    docs = {
        "doc_04_intern_checklist.md": _doc(
            "doc_04_intern_checklist",
            "[МАТЕРИАЛ КЕЙСА] Чек-лист стажёра: общие шаги адаптации",
            ["intern", "mentor", "lead"],
            disclaimer,
            intern_items,
        ),
        "doc_05_backend_checklist.md": _doc(
            "doc_05_backend_checklist",
            "[МАТЕРИАЛ КЕЙСА] Чек-лист Backend-стажёра: инструменты и материалы",
            ["intern", "mentor", "lead"],
            disclaimer,
            backend_items,
        ),
        "doc_06_mentor_checklist.md": _doc(
            "doc_06_mentor_checklist",
            "[МАТЕРИАЛ КЕЙСА] Чек-лист наставника стажёра",
            ["mentor", "lead"],
            disclaimer,
            mentor_items,
        ),
    }
    paths = []
    for name, content in docs.items():
        path = out_dir / name
        path.write_text(content, encoding="utf-8")
        paths.append(path)
    return paths


if __name__ == "__main__":  # pragma: no cover - CLI
    if len(sys.argv) != 3:
        print(__doc__)
        sys.exit(2)
    for p in build(Path(sys.argv[1]), Path(sys.argv[2])):
        print("written", p)
