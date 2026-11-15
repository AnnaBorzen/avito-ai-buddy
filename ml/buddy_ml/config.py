"""Настройки ML-сервиса из переменных окружения (секреты в логи не попадают)."""

from __future__ import annotations

import os
import re
from dataclasses import dataclass, field
from pathlib import Path

PACKAGE_DIR = Path(__file__).resolve().parent
ML_DIR = PACKAGE_DIR.parent
DEFAULT_KB_DIRS = (ML_DIR / "knowledge" / "synthetic", ML_DIR / "knowledge" / "case_materials")
DEFAULT_PLAYBOOK_DIR = ML_DIR / "data" / "external" / "playbook"


def _bool(value: str | None, default: bool = False) -> bool:
    if value is None or value.strip() == "":
        return default
    return value.strip().lower() in ("1", "true", "yes", "on")


def parse_duration(value: str | None, default: float) -> float:
    """'30s' / '1500ms' / '2m' / '12' → секунды."""
    if value is None or not value.strip():
        return default
    m = re.fullmatch(r"\s*(\d+(?:\.\d+)?)\s*(ms|s|m)?\s*", value)
    if not m:
        raise ValueError(f"invalid duration: {value!r}")
    num, unit = float(m.group(1)), m.group(2) or "s"
    return num / 1000 if unit == "ms" else num * 60 if unit == "m" else num


@dataclass
class Settings:
    port: int = 8090
    llm_enabled: bool = False
    llm_base_url: str = ""
    llm_api_key: str = field(default="", repr=False)
    llm_model: str = ""
    llm_timeout: float = 20.0
    retriever: str = "bm25"  # bm25 | bm25+tfidf | bm25+dense | bm25+tfidf+dense | dense (см. evals/reports/retrieval_comparison.md)
    embedding_model: str = "intfloat/multilingual-e5-small"
    kb_dirs: tuple[Path, ...] = DEFAULT_KB_DIRS
    playbook_dir: Path | None = DEFAULT_PLAYBOOK_DIR
    intent_min_confidence: float = 0.30
    answer_min_confidence: float = 0.6  # IDF-покрытие запроса фрагментом (см. retrieval.coverage)

    @classmethod
    def from_env(cls, env: dict | None = None) -> "Settings":
        env = dict(os.environ if env is None else env)
        s = cls()
        s.port = int(env.get("ML_PORT") or s.port)
        s.llm_enabled = _bool(env.get("LLM_ENABLED"), False)
        s.llm_base_url = env.get("LLM_BASE_URL", "") or ""
        s.llm_api_key = env.get("LLM_API_KEY", "") or ""
        s.llm_model = env.get("LLM_MODEL", "") or ""
        s.llm_timeout = parse_duration(env.get("LLM_TIMEOUT"), s.llm_timeout)
        s.retriever = env.get("ML_RETRIEVER") or s.retriever
        s.embedding_model = env.get("ML_EMBEDDING_MODEL") or s.embedding_model
        if env.get("ML_KB_DIRS"):
            s.kb_dirs = tuple(Path(p) for p in env["ML_KB_DIRS"].split(os.pathsep) if p)
        if "ML_PLAYBOOK_DIR" in env:
            s.playbook_dir = Path(env["ML_PLAYBOOK_DIR"]) if env["ML_PLAYBOOK_DIR"] else None
        if env.get("ML_INTENT_MIN_CONFIDENCE"):
            s.intent_min_confidence = float(env["ML_INTENT_MIN_CONFIDENCE"])
        if env.get("ML_ANSWER_MIN_CONFIDENCE"):
            s.answer_min_confidence = float(env["ML_ANSWER_MIN_CONFIDENCE"])
        s.validate()
        return s

    def validate(self) -> None:
        problems = []
        if self.llm_enabled and not self.llm_base_url:
            problems.append("LLM_BASE_URL is required when LLM_ENABLED=true")
        if self.llm_enabled and not self.llm_model:
            problems.append("LLM_MODEL is required when LLM_ENABLED=true")
        if self.retriever not in ("bm25", "tfidf", "dense", "bm25+tfidf", "bm25+dense", "bm25+tfidf+dense"):
            problems.append(f"unknown ML_RETRIEVER: {self.retriever}")
        if problems:
            raise ValueError("; ".join(problems))

    def public(self) -> dict:
        """Безопасное представление для /readyz и логов (без ключа)."""
        return {
            "llm_enabled": self.llm_enabled,
            "llm_model": self.llm_model if self.llm_enabled else None,
            "llm_api_key_set": bool(self.llm_api_key),
            "retriever": self.retriever,
            "embedding_model": self.embedding_model if "dense" in self.retriever else None,
        }
