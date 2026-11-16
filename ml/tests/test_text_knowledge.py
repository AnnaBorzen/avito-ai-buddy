from pathlib import Path

import pytest

from buddy_ml.knowledge import (
    Document,
    build_knowledge_base,
    chunk_document,
    load_playbook,
    make_document,
    parse_markdown,
)
from buddy_ml.text import content_tokens, normalize, stem, stems, tokenize


def test_normalize_and_tokenize():
    assert normalize("  Ёлка   ВИКИ ") == "елка вики"
    assert tokenize("Доступ к space_auto_backend_wiki!") == ["доступ", "к", "space_auto_backend_wiki"]


def test_stems_drop_stopwords_and_handle_latin():
    assert "доступ" in stems("доступа к базе")
    assert "к" not in stems("доступа к базе")
    assert stem("reviews") == "review"
    assert stem("2026") == "2026"
    assert "соглас" in "".join(content_tokens("согласование заявки"))


def test_parse_markdown_front_matter_and_defaults():
    meta, body = parse_markdown("---\ndoc_id: x\nallowed_roles: [intern]\n---\n## A\ntext", {"source_type": "s"})
    assert meta["doc_id"] == "x" and meta["source_type"] == "s"
    assert body.startswith("## A")
    meta2, body2 = parse_markdown("no front matter", {"k": 1})
    assert meta2 == {"k": 1} and body2 == "no front matter"


def test_document_is_closed_by_default():
    doc = make_document({}, "body", "fallback")
    assert doc.doc_id == "fallback"
    assert not doc.allows("intern", "team_auto_backend")
    doc2 = make_document({"allowed_roles": "lead", "allowed_teams": "*"}, "b", "f")
    assert doc2.allows("lead", "any") and not doc2.allows("intern", "any")
    assert doc2.source_label == "Тестовая инструкция"


def _doc(body: str, source_type="synthetic_instruction") -> Document:
    return make_document({"doc_id": "d", "title": "T", "allowed_roles": ["*"], "allowed_teams": ["*"],
                          "source_type": source_type}, body, "d")


def test_chunking_skips_disclaimer_and_splits_long_sections():
    long_para = "\n\n".join("слово " * 60 for _ in range(6))
    chunks = chunk_document(_doc("ВНИМАНИЕ: тест.\n\n## Раздел 1\nкороткий\n## Раздел 2\n" + long_para))
    sections = [c.section for c in chunks]
    assert "T" not in sections  # преамбула-дисклеймер не индексируется
    assert sections.count("Раздел 2") >= 2
    assert all(len(c.text) <= 1200 for c in chunks)


def test_chunking_without_headings_and_with_preamble():
    assert chunk_document(_doc("просто текст"))[0].section == "T"
    chunks = chunk_document(_doc("Вступление по делу\n## A\nтекст"))
    assert chunks[0].section == "T" and chunks[0].text == "Вступление по делу"


def test_chunking_very_long_paragraph_is_cut():
    chunks = chunk_document(_doc("## A\n" + "x" * 3000))
    assert len(chunks) >= 3


def test_playbook_short_sections_are_merged(tmp_path: Path):
    (tmp_path / "spt.md").write_text(
        "# SPT: работа с обращениями\n## Типы задач\n" + "Описание типов задач. " * 10 + "\n## Пусто\n", encoding="utf-8"
    )
    docs = load_playbook(tmp_path)
    assert docs[0].doc_id == "playbook_spt" and docs[0].source_type == "public_playbook"
    assert docs[0].allows("intern", "team_goods_search")
    chunks = chunk_document(docs[0])
    assert chunks[0].text.startswith("SPT: работа с обращениями")  # заголовок приклеен к секции
    assert chunks[-1].section == docs[0].title  # хвостовой пустой заголовок не потерян


def test_knowledge_base_acl_and_duplicates(tmp_path: Path):
    kb = build_knowledge_base([Path(__file__).resolve().parents[1] / "knowledge" / "synthetic"])
    ids = {d.doc_id for d in kb.documents}
    assert ids == {"doc_01_onboarding_access", "doc_02_security_restrictions", "doc_03_lead_internal_policy"}
    intern = {c.doc.doc_id for c in kb.allowed_chunks("intern", "team_auto_backend")}
    assert "doc_03_lead_internal_policy" not in intern
    assert {c.doc.doc_id for c in kb.forbidden_chunks("intern", "team_auto_backend")} == {"doc_03_lead_internal_policy"}
    assert "doc_03_lead_internal_policy" in {c.doc.doc_id for c in kb.allowed_chunks("lead", "team_auto_backend")}
    assert "doc_03_lead_internal_policy" not in {c.doc.doc_id for c in kb.allowed_chunks("lead", "team_goods_search")}
    assert kb.get("doc_01_onboarding_access") is not None and kb.get("nope") is None
    assert kb.section("doc_01_onboarding_access", "Какие параметры") is not None
    assert kb.section("doc_01_onboarding_access", "Нет такой") is None

    d = tmp_path / "dup"
    d.mkdir()
    for name in ("a.md", "b.md"):
        (d / name).write_text("---\ndoc_id: same\n---\ntext", encoding="utf-8")
    with pytest.raises(ValueError, match="duplicate"):
        build_knowledge_base([d])


def test_knowledge_base_missing_dirs_are_ignored(tmp_path: Path):
    kb = build_knowledge_base([tmp_path / "absent"], playbook_dir=tmp_path / "absent2")
    assert kb.documents == [] and kb.chunks == []
