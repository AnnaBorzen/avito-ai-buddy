"""Скачивает открытый AvitoTech Playbook (github.com/avito-tech/playbook) для RAG.

В репозитории playbook нет файла лицензии, поэтому мы не коммитим его копию, а
скачиваем зафиксированный коммит при сборке образа / локально. Без него сервис
работает (используются только тестовые инструкции и чек-лист из материалов кейса).

    python ml/scripts/fetch_playbook.py [target_dir]
"""

from __future__ import annotations

import io
import sys
import urllib.request
import zipfile
from pathlib import Path

COMMIT = "0c1d2aa1501ca6889357908f54b12695f0534e48"
URL = f"https://codeload.github.com/avito-tech/playbook/zip/{COMMIT}"
DEFAULT_TARGET = Path(__file__).resolve().parents[1] / "data" / "external" / "playbook"


def fetch(target: Path = DEFAULT_TARGET, url: str = URL, timeout: float = 60.0) -> int:
    with urllib.request.urlopen(url, timeout=timeout) as resp:  # noqa: S310 - фиксированный https URL
        data = resp.read()
    target.mkdir(parents=True, exist_ok=True)
    n = 0
    with zipfile.ZipFile(io.BytesIO(data)) as zf:
        for name in zf.namelist():
            # берём только markdown верхнего уровня, без путей вида ../
            parts = Path(name).parts
            if len(parts) != 2 or not name.endswith(".md") or parts[1].lower() == "readme.md":
                continue
            (target / parts[1]).write_bytes(zf.read(name))
            n += 1
    (target / "SOURCE.txt").write_text(f"{url}\n", encoding="utf-8")
    return n


if __name__ == "__main__":  # pragma: no cover - CLI
    dest = Path(sys.argv[1]) if len(sys.argv) > 1 else DEFAULT_TARGET
    try:
        print(f"playbook: {fetch(dest)} files -> {dest}")
    except Exception as exc:  # сеть недоступна — сервис работает и без playbook
        print(f"playbook fetch skipped: {exc}", file=sys.stderr)
