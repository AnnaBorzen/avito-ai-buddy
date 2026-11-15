"""Сравнение конфигураций поиска на evals/tasks/retrieval.jsonl.

    python ml/scripts/compare_retrievers.py [--models m1,m2] [--out evals/reports/retrieval_comparison.json]

Dense-модели требуют requirements-dense.txt (torch CPU + sentence-transformers).
"""
from __future__ import annotations

import argparse
import json
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "ml"))

from buddy_ml.config import Settings  # noqa: E402
from buddy_ml.evaluation import eval_retrieval, load_jsonl  # noqa: E402
from buddy_ml.knowledge import build_knowledge_base  # noqa: E402
from buddy_ml.retrieval import Retriever  # noqa: E402


def conclusion(summary: dict) -> str:
    """Вывод формируется из данных, а не пишется руками."""
    best = max(summary, key=lambda k: (summary[k]["mrr@5"], -summary[k]["query_ms"]))
    b = summary[best]
    text = (f"Вывод: лучшая конфигурация по MRR@5 — `{best}` (Recall@1 {b['recall@1']:.3f}, MRR {b['mrr@5']:.3f}, "
            f"{b['query_ms']} мс на запрос).")
    dense = {k: v for k, v in summary.items() if "dense" in k}
    if dense and "dense" not in best:
        d = max(dense, key=lambda k: dense[k]["mrr@5"])
        text += (f" Лучшая конфигурация с эмбеддингами (`{d}`, MRR {dense[d]['mrr@5']:.3f}) не превзошла её, требуя torch "
                 f"(+~2 ГБ образа) и {dense[d]['query_ms']} мс на запрос, поэтому по умолчанию используется лексический поиск, "
                 "а dense включается через `ML_RETRIEVER` при росте корпуса и доли перефразированных запросов.")
    return text


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--models", default="cointegrated/rubert-tiny2,intfloat/multilingual-e5-small")
    ap.add_argument("--out", default=str(ROOT / "evals" / "reports" / "retrieval_comparison.json"))
    args = ap.parse_args()
    s = Settings()
    kb = build_knowledge_base(list(s.kb_dirs), s.playbook_dir)
    tasks = load_jsonl(ROOT / "evals" / "tasks" / "retrieval.jsonl")
    results = {}
    configs = [("bm25", None), ("tfidf", None), ("bm25+tfidf", None)]
    encoders = {}
    if args.models:
        from buddy_ml.retrieval import SentenceTransformerEncoder
        for m in args.models.split(","):
            t = time.perf_counter()
            encoders[m] = SentenceTransformerEncoder(m)
            print(f"loaded {m} in {time.perf_counter() - t:.1f}s", flush=True)
            configs += [(f"dense[{m}]", m), (f"bm25+dense[{m}]", m), (f"bm25+tfidf+dense[{m}]", m)]
    for name, m in configs:
        t = time.perf_counter()
        rankers = tuple(name.split("[")[0].split("+"))
        r = Retriever(kb, encoder=encoders.get(m), rankers=rankers)
        build = time.perf_counter() - t
        t = time.perf_counter()
        res = eval_retrieval(r, tasks)
        q_ms = (time.perf_counter() - t) / len(tasks) * 1000
        res.update({"index_build_s": round(build, 2), "query_ms": round(q_ms, 1)})
        results[name] = res
        print(f"{name:55s} R@1={res['recall@1']:.3f} R@3={res['recall@3']:.3f} MRR={res['mrr@5']:.3f} "
              f"leaks={res['acl_leaks']} build={build:.1f}s q={q_ms:.1f}ms", flush=True)
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    summary = {k: {kk: vv for kk, vv in v.items() if kk != "per_query"} for k, v in results.items()}
    out.write_text(json.dumps({"chunks": len(kb.chunks), "documents": len(kb.documents), "summary": summary,
                               "details": results}, ensure_ascii=False, indent=2), encoding="utf-8")
    typo_ids = {t["id"] for t in tasks if "typo" in t.get("tags", [])}
    md = ["# Сравнение конфигураций поиска", "",
          f"Корпус: {len(kb.documents)} документов, {len(kb.chunks)} фрагментов. Запросов с разметкой: "
          f"{next(iter(summary.values()))['queries']} (из них с опечатками: {len(typo_ids)}). "
          "Пользователь — usr_intern_01, ACL применяется до ранжирования.", "",
          "| Конфигурация | Recall@1 | Recall@3 | MRR@5 | Утечки ACL | Индекс, с | Запрос, мс |", "|---|---|---|---|---|---|---|"]
    for name, v in summary.items():
        md.append(f"| `{name}` | {v['recall@1']:.3f} | {v['recall@3']:.3f} | {v['mrr@5']:.3f} | {v['acl_leaks']} | "
                  f"{v['index_build_s']} | {v['query_ms']} |")
    md += ["", "Воспроизвести: `pip install -r ml/requirements-dense.txt && python ml/scripts/compare_retrievers.py`.", "",
           conclusion(summary)]
    out.with_suffix(".md").write_text("\n".join(md) + "\n", encoding="utf-8")
    print("written", out)


if __name__ == "__main__":
    main()
