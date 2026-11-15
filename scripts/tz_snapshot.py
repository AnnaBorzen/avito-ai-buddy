"""Экспорт внешнего эталона `Авито_ТЗ.docx` в дословный markdown-снимок.

Запуск из корня репозитория::

    python3 scripts/tz_snapshot.py

Параграфы и таблицы выгружаются из `word/document.xml` в исходном порядке.
Одноколоночные таблицы (оформленные в исходнике как блоки кода) рендерятся
fenced-блоком, остальные — markdown-таблицей. Текст не редактируется.

Правила сверки и карта разделов — `docs/tz/README.md`.
"""

import hashlib
import re
import sys
import zipfile
from pathlib import Path
from xml.etree import ElementTree as ET

W = "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}"

ROOT = Path(__file__).resolve().parent.parent
SRC = ROOT / "\u0410\u0432\u0438\u0442\u043e_\u0422\u0417.docx"
DST = ROOT / "docs" / "tz" / "avito-tz.md"

if not SRC.exists():
    sys.exit(
        f"Не найден эталон требований: {SRC}\n"
        "Файл \u0410\u0432\u0438\u0442\u043e_\u0422\u0417.docx в репозиторий не коммитится; "
        "положите его в корень репозитория перед генерацией снимка."
    )


def para_text(p):
    out = []
    for node in p.iter():
        if node.tag == W + "t":
            out.append(node.text or "")
        elif node.tag == W + "tab":
            out.append(" ")
        elif node.tag == W + "br" or node.tag == W + "cr":
            out.append("\n")
    return "".join(out).strip()


def cell_paragraphs(tc):
    return [x for x in (para_text(p) for p in tc.iter(W + "p")) if x]


def cell_text(tc):
    return " / ".join(cell_paragraphs(tc)).replace("|", "\\|")


with open(SRC, "rb") as fh:
    raw = fh.read()
md5 = hashlib.md5(raw).hexdigest()

EXPECTED_MD5 = "4fdcf6f6b5465c3d700d4e409491c577"

root = ET.fromstring(zipfile.ZipFile(SRC).read("word/document.xml"))
body = root.find(W + "body")

HEADING = re.compile(r"^\d+(?:\.\d+)*\.?\s+\S")

lines = []
tables = 0
for child in body:
    if child.tag == W + "p":
        text = para_text(child)
        if not text:
            continue
        lines.append(("## " + text) if HEADING.match(text) and len(text) < 160 else text)
        lines.append("")
    elif child.tag == W + "tbl":
        raw_rows = []
        for tr in child.findall(W + "tr"):
            raw_rows.append([cell_paragraphs(tc) for tc in tr.findall(W + "tc")])
        raw_rows = [r for r in raw_rows if any(any(c) for c in r)]
        if not raw_rows:
            continue
        tables += 1
        width = max(len(r) for r in raw_rows)
        if width == 1:
            block = []
            for row in raw_rows:
                for cell in row:
                    for text in cell:
                        block.extend(line for line in text.split("\n") if line.strip())
            if block:
                lines.append("```text")
                lines.extend(block)
                lines.append("```")
                lines.append("")
            continue
        rows = []
        for row in raw_rows:
            cells = [" / ".join(c).replace("|", "\\|") for c in row]
            rows.append(cells + [""] * (width - len(cells)))
        lines.append("| " + " | ".join(rows[0]) + " |")
        lines.append("|" + "---|" * width)
        for row in rows[1:]:
            lines.append("| " + " | ".join(row) + " |")
        lines.append("")

header = [
    "# \u0410\u0432\u0438\u0442\u043e_\u0422\u0417.docx \u2014 \u0434\u043e\u0441\u043b\u043e\u0432\u043d\u044b\u0439 \u0441\u043d\u0438\u043c\u043e\u043a \u0442\u0435\u043a\u0441\u0442\u0430",
    "",
    "> \u0421\u043b\u0443\u0436\u0435\u0431\u043d\u044b\u0439 \u0444\u0430\u0439\u043b. \u0418\u0441\u0442\u043e\u0447\u043d\u0438\u043a \u2014 \u0432\u043d\u0435\u0448\u043d\u0438\u0439 `\u0410\u0432\u0438\u0442\u043e_\u0422\u0417.docx` (\u0432 \u0440\u0435\u043f\u043e\u0437\u0438\u0442\u043e\u0440\u0438\u0439 \u043d\u0435 \u043a\u043e\u043c\u043c\u0438\u0442\u0438\u0442\u0441\u044f).",
    "> \u0412\u044b\u0433\u0440\u0443\u0436\u0435\u043d\u043e \u0438\u0437 `word/document.xml`: \u043f\u0430\u0440\u0430\u0433\u0440\u0430\u0444\u044b \u0438 \u0442\u0430\u0431\u043b\u0438\u0446\u044b \u0432 \u0438\u0441\u0445\u043e\u0434\u043d\u043e\u043c \u043f\u043e\u0440\u044f\u0434\u043a\u0435, \u0442\u0435\u043a\u0441\u0442 \u043d\u0435 \u0440\u0435\u0434\u0430\u043a\u0442\u0438\u0440\u043e\u0432\u0430\u043b\u0441\u044f.",
    f"> md5 \u0438\u0441\u0445\u043e\u0434\u043d\u043e\u0433\u043e \u0444\u0430\u0439\u043b\u0430: `{md5}`",
    f"> \u041e\u0431\u044a\u0451\u043c: {len(raw)} \u0431\u0430\u0439\u0442, {tables} \u0442\u0430\u0431\u043b\u0438\u0446.",
    "> \u041f\u0440\u0430\u0432\u0438\u043b\u0430 \u0441\u0432\u0435\u0440\u043a\u0438 \u0438 \u043e\u0431\u043d\u043e\u0432\u043b\u0435\u043d\u0438\u044f \u2014 [`README.md`](README.md).",
    "",
    "---",
    "",
]
with open(DST, "w", encoding="utf-8") as fh:
    fh.write("\n".join(header + lines).rstrip() + "\n")

print("md5:", md5, "tables:", tables, "lines:", len(lines))
if md5 != EXPECTED_MD5:
    print(
        "ВНИМАНИЕ: md5 источника отличается от зафиксированного "
        f"{EXPECTED_MD5}. Пересмотрите карту разделов в docs/tz/README.md "
        "и артефакты, перечисленные в ней.",
        file=sys.stderr,
    )
    sys.exit(2)
