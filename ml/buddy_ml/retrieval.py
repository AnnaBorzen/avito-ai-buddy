"""Гибридный поиск по базе знаний с фильтрацией по правам ДО ранжирования.

Ранкеры:
  * BM25 по русским стемам (точные термины: `space_auto_backend_wiki`, «согласование»);
  * TF-IDF по символьным n-граммам (устойчив к опечаткам и словоформам);
  * опционально dense-эмбеддинги (sentence-transformers, напр. rubert-tiny2 / multilingual-e5).
Результаты объединяются Reciprocal Rank Fusion. Уверенность (confidence) — абсолютная
косинусная близость лучшего фрагмента; используется для отказа «нет данных» (AC-05).
"""

from __future__ import annotations

import math
from collections import Counter
from dataclasses import dataclass, field
from typing import Protocol

import numpy as np
from sklearn.feature_extraction.text import TfidfVectorizer

from .knowledge import Chunk, KnowledgeBase
from .text import normalize, stems

RRF_K = 60
# Веса ранкеров в RRF подобраны на evals/tasks/retrieval.jsonl (см. evals/reports/retrieval_comparison.md):
# BM25 точнее на коротком корпусе, TF-IDF по символам добавляет устойчивость к опечаткам.
DEFAULT_WEIGHTS = {"bm25": 1.0, "tfidf": 0.25, "dense": 1.0}


class Encoder(Protocol):  # pragma: no cover - протокол
    name: str

    def encode(self, texts: list[str], is_query: bool = False) -> np.ndarray: ...


@dataclass
class Hit:
    chunk: Chunk
    score: float
    bm25: float = 0.0
    tfidf: float = 0.0
    dense: float = 0.0
    coverage: float = 0.0  # IDF-взвешенная доля терминов запроса, найденных во фрагменте

    def to_source(self, excerpt_chars: int = 400) -> dict:
        doc = self.chunk.doc
        excerpt = self.chunk.text
        if len(excerpt) > excerpt_chars:
            excerpt = excerpt[:excerpt_chars].rsplit(" ", 1)[0] + "…"
        return {
            "doc_id": doc.doc_id,
            "title": doc.title,
            "section": self.chunk.section,
            "excerpt": excerpt,
            "is_synthetic": doc.is_synthetic,
            "source_type": doc.source_type,
            "source_label": doc.source_label,
            "source_url": doc.source_url,
            "score": round(float(self.score), 4),
        }


@dataclass
class SearchResult:
    hits: list[Hit] = field(default_factory=list)
    confidence: float = 0.0

    @property
    def top(self) -> Hit | None:
        return self.hits[0] if self.hits else None


class BM25:
    def __init__(self, docs: list[list[str]], k1: float = 1.5, b: float = 0.75):
        self.k1, self.b = k1, b
        self.docs = docs
        self.lengths = np.array([len(d) for d in docs], dtype=float)
        self.avgdl = float(self.lengths.mean()) if len(docs) else 0.0
        self.tf = [Counter(d) for d in docs]
        df: Counter = Counter()
        for d in docs:
            df.update(set(d))
        n = len(docs)
        self.idf = {t: math.log(1 + (n - f + 0.5) / (f + 0.5)) for t, f in df.items()}

    def scores(self, query: list[str], idx: np.ndarray) -> np.ndarray:
        out = np.zeros(len(idx))
        if not self.avgdl:
            return out
        for j, i in enumerate(idx):
            tf, dl, s = self.tf[i], self.lengths[i], 0.0
            for term in query:
                f = tf.get(term)
                if not f:
                    continue
                denom = f + self.k1 * (1 - self.b + self.b * dl / self.avgdl)
                s += self.idf[term] * f * (self.k1 + 1) / denom
            out[j] = s
        return out


class Retriever:
    def __init__(
        self,
        kb: KnowledgeBase,
        encoder: Encoder | None = None,
        rankers: tuple[str, ...] = ("bm25", "tfidf", "dense"),
    ):
        self.kb = kb
        self.chunks = kb.chunks
        self.encoder = encoder
        self.rankers = tuple(r for r in rankers if r != "dense" or encoder is not None)
        texts = [normalize(c.search_text + " " + c.doc.title) for c in self.chunks]
        self.bm25 = BM25([stems(t) for t in texts])
        self.tfidf = TfidfVectorizer(analyzer="char_wb", ngram_range=(3, 5), sublinear_tf=True)
        self.tfidf_matrix = self.tfidf.fit_transform(texts) if texts else None
        self.dense_matrix = None
        if encoder is not None and self.chunks:
            self.dense_matrix = encoder.encode([c.search_text for c in self.chunks])
        self._pos = {c.chunk_id: i for i, c in enumerate(self.chunks)}
        self._vocab: dict[tuple[str, str], set[str]] = {}
        self.weights = dict(DEFAULT_WEIGHTS)

    @property
    def mode(self) -> str:
        return "+".join(self.rankers)

    def _allowed_idx(self, role: str, team_id: str, doc_ids: set[str] | None) -> np.ndarray:
        idx = [
            i
            for i, c in enumerate(self.chunks)
            if c.doc.allows(role, team_id) and (doc_ids is None or c.doc.doc_id in doc_ids)
        ]
        return np.array(idx, dtype=int)

    def search(
        self,
        query: str,
        role: str,
        team_id: str,
        k: int = 3,
        doc_ids: set[str] | None = None,
    ) -> SearchResult:
        idx = self._allowed_idx(role, team_id, doc_ids)
        if not len(idx) or not query.strip():
            return SearchResult()
        q_norm = normalize(query)
        ranked: dict[str, np.ndarray] = {}
        if "bm25" in self.rankers:
            ranked["bm25"] = self.bm25.scores(stems(q_norm), idx)
        if "tfidf" in self.rankers:
            qv = self.tfidf.transform([q_norm])
            ranked["tfidf"] = (self.tfidf_matrix[idx] @ qv.T).toarray().ravel()
        if "dense" in self.rankers:
            qd = self.encoder.encode([query], is_query=True)[0]
            ranked["dense"] = self.dense_matrix[idx] @ qd

        fused = np.zeros(len(idx))
        for name, scores in ranked.items():
            order = np.argsort(-scores, kind="stable")
            ranks = np.empty(len(idx))
            ranks[order] = np.arange(1, len(idx) + 1)
            contrib = 1.0 / (RRF_K + ranks)
            contrib[scores <= 0] = 0.0  # нулевое совпадение не голосует
            fused += self.weights.get(name, 1.0) * contrib
        order = np.argsort(-fused, kind="stable")[:k]
        hits = []
        for j in order:
            if fused[j] <= 0:
                continue
            hits.append(
                Hit(
                    chunk=self.chunks[idx[j]],
                    score=float(fused[j]),
                    bm25=float(ranked.get("bm25", np.zeros(len(idx)))[j]),
                    tfidf=float(ranked.get("tfidf", np.zeros(len(idx)))[j]),
                    dense=float(ranked.get("dense", np.zeros(len(idx)))[j]),
                )
            )
        q_terms = {t for t in stems(q_norm) if len(t) > 2}
        for h in hits:
            h.coverage = self.coverage(q_terms, self._pos[h.chunk.chunk_id])
        confidence = 0.0
        if hits:
            # dense-косинус rubert-tiny2 для релевантных пар обычно > 0.6; приводим к шкале покрытия
            confidence = max(max(h.coverage for h in hits), max((h.dense - 0.35) / 0.5 for h in hits))
        return SearchResult(hits=hits, confidence=float(min(max(confidence, 0.0), 1.0)))

    def oov_ratio(self, query: str, role: str, team_id: str) -> float:
        """IDF-взвешенная доля терминов запроса, отсутствующих во ВСЕХ доступных пользователю фрагментах."""
        q_terms = {t for t in stems(normalize(query)) if len(t) > 2}
        if not q_terms:
            return 0.0
        key = (role, team_id)
        if key not in self._vocab:
            vocab: set[str] = set()
            for i in self._allowed_idx(role, team_id, None):
                vocab.update(self.bm25.tf[i])
            self._vocab[key] = vocab
        vocab = self._vocab[key]
        # Неизвестный корпусу термин весит как медианный: на маленьком корпусе даже «сколько»
        # или «ждать» отсутствуют, и максимальный вес давал бы ложные отказы.
        idfs = sorted(self.bm25.idf.values()) or [1.0]
        median_idf = idfs[len(idfs) // 2]
        weights = {t: self.bm25.idf.get(t, median_idf) for t in q_terms}
        total = sum(weights.values())
        return sum(w for t, w in weights.items() if t not in vocab) / total

    def coverage(self, q_terms: set[str], i: int) -> float:
        """Доля «веса» запроса, покрытая фрагментом. Неизвестный корпусу термин имеет макс. вес,
        поэтому вопрос про «погоду» или «VPN» получает низкую уверенность → отказ (AC-05)."""
        if not q_terms:
            return 0.0
        max_idf = max(self.bm25.idf.values(), default=1.0)
        doc_terms = self.bm25.tf[i]
        total = sum(self.bm25.idf.get(t, max_idf) for t in q_terms)
        hit = sum(self.bm25.idf.get(t, max_idf) for t in q_terms if t in doc_terms)
        return hit / total if total else 0.0


class SentenceTransformerEncoder:  # pragma: no cover - требует тяжёлых зависимостей, проверяется в evals
    """Dense-энкодер. Для e5-моделей автоматически добавляет префиксы query:/passage:."""

    def __init__(self, model_name: str):
        from sentence_transformers import SentenceTransformer

        self.name = model_name
        self.model = SentenceTransformer(model_name, device="cpu")
        self.e5 = "e5" in model_name.lower()

    def encode(self, texts: list[str], is_query: bool = False) -> np.ndarray:
        if self.e5:
            prefix = "query: " if is_query else "passage: "
            texts = [prefix + t for t in texts]
        return np.asarray(
            self.model.encode(texts, normalize_embeddings=True, batch_size=32, show_progress_bar=False)
        )
