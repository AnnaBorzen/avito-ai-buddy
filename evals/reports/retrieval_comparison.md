# Сравнение конфигураций поиска

Корпус: 33 документов, 680 фрагментов. Запросов с разметкой: 64 (из них с опечатками: 6). Пользователь — usr_intern_01, ACL применяется до ранжирования.

| Конфигурация | Recall@1 | Recall@3 | MRR@5 | Утечки ACL | Индекс, с | Запрос, мс |
|---|---|---|---|---|---|---|
| `bm25` | 0.891 | 0.953 | 0.922 | 0 | 1.06 | 1.4 |
| `tfidf` | 0.797 | 0.922 | 0.850 | 0 | 0.84 | 12.6 |
| `bm25+tfidf` | 0.875 | 0.953 | 0.914 | 0 | 1.43 | 12.2 |
| `dense[cointegrated/rubert-tiny2]` | 0.406 | 0.609 | 0.520 | 0 | 9.16 | 8.0 |
| `bm25+dense[cointegrated/rubert-tiny2]` | 0.797 | 0.906 | 0.848 | 0 | 8.57 | 9.2 |
| `bm25+tfidf+dense[cointegrated/rubert-tiny2]` | 0.766 | 0.938 | 0.844 | 0 | 8.3 | 19.0 |
| `dense[intfloat/multilingual-e5-small]` | 0.797 | 0.859 | 0.833 | 0 | 49.63 | 30.4 |
| `bm25+dense[intfloat/multilingual-e5-small]` | 0.844 | 0.906 | 0.883 | 0 | 48.7 | 26.5 |
| `bm25+tfidf+dense[intfloat/multilingual-e5-small]` | 0.844 | 0.953 | 0.894 | 0 | 51.0 | 38.6 |

Воспроизвести: `pip install -r ml/requirements-dense.txt && python ml/scripts/compare_retrievers.py`.

Вывод: лучшая конфигурация по MRR@5 — `bm25` (Recall@1 0.891, MRR 0.922, 1.4 мс на запрос). Лучшая конфигурация с эмбеддингами (`bm25+tfidf+dense[intfloat/multilingual-e5-small]`, MRR 0.894) не превзошла её, требуя torch (+~2 ГБ образа) и 38.6 мс на запрос, поэтому по умолчанию используется лексический поиск, а dense включается через `ML_RETRIEVER` при росте корпуса и доли перефразированных запросов.
