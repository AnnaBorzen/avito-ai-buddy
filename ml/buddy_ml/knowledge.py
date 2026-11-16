"""Загрузка базы знаний (Markdown + front matter), разбиение на фрагменты и ACL.

Каждый документ несёт метаданные доступа (allowed_roles / allowed_teams). Фильтрация
по ним выполняется ДО поиска и до передачи контекста в LLM (FR-02, FR-09).
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from pathlib import Path

import yaml

SOURCE_LABELS = {
    "synthetic_instruction": "Тестовая инструкция",
    "case_material": "Материал кейса",
    "public_playbook": "Открытый источник",
}

MAX_CHUNK_CHARS = 1200


@dataclass(frozen=True)
class Document:
    doc_id: str
    title: str
    allowed_roles: tuple[str, ...]
    allowed_teams: tuple[str, ...]
    is_synthetic: bool
    source_type: str
    source_url: str | None
    body: str

    def allows(self, role: str, team_id: str) -> bool:
        role_ok = role in self.allowed_roles or "*" in self.allowed_roles
        team_ok = "*" in self.allowed_teams or team_id in self.allowed_teams
        return role_ok and team_ok

    @property
    def source_label(self) -> str:
        return SOURCE_LABELS.get(self.source_type, "Источник")


@dataclass(frozen=True)
class Chunk:
    chunk_id: str
    doc: Document
    section: str
    text: str

    @property
    def search_text(self) -> str:
        # Заголовок секции и документа сильно помогают лексическому поиску.
        return f"{self.section}. {self.text}"


@dataclass
class KnowledgeBase:
    documents: list[Document] = field(default_factory=list)
    chunks: list[Chunk] = field(default_factory=list)

    def allowed_chunks(self, role: str, team_id: str) -> list[Chunk]:
        return [c for c in self.chunks if c.doc.allows(role, team_id)]

    def forbidden_chunks(self, role: str, team_id: str) -> list[Chunk]:
        return [c for c in self.chunks if not c.doc.allows(role, team_id)]

    def get(self, doc_id: str) -> Document | None:
        return next((d for d in self.documents if d.doc_id == doc_id), None)

    def section(self, doc_id: str, section_prefix: str) -> Chunk | None:
        for c in self.chunks:
            if c.doc.doc_id == doc_id and c.section.startswith(section_prefix):
                return c
        return None


_FRONT_RE = re.compile(r"^---\s*\n(.*?)\n---\s*\n(.*)$", re.S)


def parse_markdown(text: str, default_meta: dict | None = None) -> tuple[dict, str]:
    match = _FRONT_RE.match(text.lstrip("﻿"))
    if not match:
        return dict(default_meta or {}), text
    meta = yaml.safe_load(match.group(1)) or {}
    merged = dict(default_meta or {})
    merged.update(meta)
    return merged, match.group(2)


def make_document(meta: dict, body: str, fallback_id: str) -> Document:
    def _tuple(value, default):
        if value is None:
            return default
        if isinstance(value, str):
            return (value,)
        return tuple(str(v) for v in value)

    return Document(
        doc_id=str(meta.get("doc_id") or fallback_id),
        title=str(meta.get("title") or fallback_id),
        # По умолчанию документ закрыт: без явных ролей его никто не видит.
        allowed_roles=_tuple(meta.get("allowed_roles"), ()),
        allowed_teams=_tuple(meta.get("allowed_teams"), ()),
        is_synthetic=bool(meta.get("is_synthetic", False)),
        source_type=str(meta.get("source_type") or "synthetic_instruction"),
        source_url=meta.get("source_url"),
        body=body,
    )


def _split_long(text: str, limit: int = MAX_CHUNK_CHARS) -> list[str]:
    if len(text) <= limit:
        return [text]
    parts, current = [], ""
    for para in re.split(r"\n\s*\n|\n(?=[-*|]|\d+\.)", text):
        para = para.strip()
        if not para:
            continue
        if current and len(current) + len(para) + 1 > limit:
            parts.append(current)
            current = para
        else:
            current = f"{current}\n{para}" if current else para
        while len(current) > limit:  # очень длинный абзац
            parts.append(current[:limit])
            current = current[limit:]
    if current:
        parts.append(current)
    return parts


def chunk_document(doc: Document) -> list[Chunk]:
    """Режет документ по заголовкам ##/### (секция = единица цитирования)."""
    chunks: list[Chunk] = []
    heading_re = re.compile(r"^(#{1,3})\s+(.+?)\s*$", re.M)
    matches = list(heading_re.finditer(doc.body))
    sections: list[tuple[str, str]] = []
    if not matches:
        sections.append((doc.title, doc.body))
    else:
        pre = doc.body[: matches[0].start()].strip()
        # Преамбулу с дисклеймером («ВНИМАНИЕ…») не индексируем как знание.
        if pre and not pre.startswith(("ВНИМАНИЕ", "Источник:")):
            sections.append((doc.title, pre))
        for i, m in enumerate(matches):
            end = matches[i + 1].start() if i + 1 < len(matches) else len(doc.body)
            sections.append((m.group(2).strip(), doc.body[m.end() : end].strip()))
    if doc.source_type == "public_playbook":
        # Заголовки без текста («# SPT» → «## Типы задач») приклеиваем к следующей секции,
        # иначе пустой фрагмент выигрывает поиск по заголовку, но не несёт ответа.
        merged: list[tuple[str, str]] = []
        carry = ""
        for title, body in sections:
            if len(body) < 80:
                carry = f"{carry} {title}. {body}".strip()
                continue
            merged.append((title, f"{carry}\n{body}".strip() if carry else body))
            carry = ""
        if carry:
            merged.append((doc.title, carry))
        sections = merged
    n = 0
    for title, body in sections:
        if not body and title == doc.title:
            continue
        for piece in _split_long(body or title):
            n += 1
            chunks.append(Chunk(f"{doc.doc_id}#{n}", doc, title, piece.strip()))
    return chunks


def load_directory(path: Path, default_meta: dict | None = None) -> list[Document]:
    docs = []
    for file in sorted(Path(path).glob("**/*.md")):
        if file.name.lower() == "readme.md" and default_meta is None:
            continue
        meta, body = parse_markdown(file.read_text(encoding="utf-8"), default_meta)
        docs.append(make_document(meta, body, fallback_id=file.stem))
    return docs


PLAYBOOK_META = {
    "allowed_roles": ["intern", "mentor", "lead"],
    "allowed_teams": ["*"],
    "is_synthetic": False,
    "source_type": "public_playbook",
}


def load_playbook(path: Path) -> list[Document]:
    """Открытый AvitoTech Playbook (github.com/avito-tech/playbook), если скачан."""
    docs = []
    for file in sorted(Path(path).glob("*.md")):
        text = file.read_text(encoding="utf-8")
        title_match = re.search(r"^#\s+(.+)$", text, re.M)
        meta = dict(PLAYBOOK_META)
        meta["doc_id"] = f"playbook_{file.stem}"
        meta["title"] = f"[ОТКРЫТЫЙ ИСТОЧНИК] AvitoTech Playbook: {title_match.group(1).strip() if title_match else file.stem}"
        meta["source_url"] = f"https://github.com/avito-tech/playbook/blob/master/{file.name}"
        docs.append(make_document(meta, text, fallback_id=file.stem))
    return docs


def build_knowledge_base(dirs: list[Path], playbook_dir: Path | None = None) -> KnowledgeBase:
    kb = KnowledgeBase()
    for d in dirs:
        if Path(d).exists():
            kb.documents.extend(load_directory(Path(d)))
    if playbook_dir and Path(playbook_dir).exists():
        kb.documents.extend(load_playbook(Path(playbook_dir)))
    ids = [d.doc_id for d in kb.documents]
    dupes = {i for i in ids if ids.count(i) > 1}
    if dupes:
        raise ValueError(f"duplicate doc_id in knowledge base: {sorted(dupes)}")
    for doc in kb.documents:
        kb.chunks.extend(chunk_document(doc))
    return kb
