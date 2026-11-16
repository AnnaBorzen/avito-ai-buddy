"""NLU: классификация интента и извлечение параметров заявки (слотов).

* Интент — TF-IDF (стемы 1-2-граммы + символьные 2-5-граммы) + логистическая регрессия.
  Модель обучается при старте (< 1 с) на nlu_data.py с фиксированным seed — артефакт
  воспроизводим и не требует хранения бинарников.
* Слоты — правила с нечётким сопоставлением ключевых слов. Обязательные параметры
  НИКОГДА не подставляются по умолчанию: если уровень доступа или обоснование не названы,
  агент задаёт уточняющий вопрос (раздел 8.1 ТЗ).
"""

from __future__ import annotations

import difflib
import re
from dataclasses import dataclass, field

import numpy as np
from sklearn.feature_extraction.text import TfidfVectorizer
from sklearn.linear_model import LogisticRegression
from sklearn.pipeline import FeatureUnion, Pipeline

from .nlu_data import training_set
from .policy import CATALOG, UserContext
from .text import normalize, stems

JUSTIFICATION_MIN, JUSTIFICATION_MAX = 10, 300


# ---------------------------------------------------------------- intent classifier


def _stem_analyzer(text: str) -> list[str]:
    toks = stems(text, drop_stopwords=False)
    return toks + [f"{a}_{b}" for a, b in zip(toks, toks[1:])]


@dataclass
class IntentPrediction:
    intent: str
    confidence: float
    ranking: list[tuple[str, float]] = field(default_factory=list)


class IntentClassifier:
    def __init__(self, c: float = 8.0, seed: int = 13):
        self.pipeline = Pipeline(
            [
                (
                    "features",
                    FeatureUnion(
                        [
                            ("stems", TfidfVectorizer(analyzer=_stem_analyzer, sublinear_tf=True)),
                            (
                                "chars",
                                TfidfVectorizer(
                                    analyzer="char_wb", ngram_range=(2, 5), sublinear_tf=True, min_df=1
                                ),
                            ),
                        ]
                    ),
                ),
                (
                    "clf",
                    LogisticRegression(
                        C=c, max_iter=5000, class_weight="balanced", random_state=seed
                    ),
                ),
            ]
        )
        self.fitted = False

    def fit(self, texts: list[str] | None = None, labels: list[str] | None = None) -> "IntentClassifier":
        if texts is None or labels is None:
            texts, labels = training_set()
        self.pipeline.fit([normalize(t) for t in texts], labels)
        self.fitted = True
        return self

    @property
    def classes(self) -> list[str]:
        return list(self.pipeline.classes_)

    def predict(self, text: str) -> IntentPrediction:
        probs = self.pipeline.predict_proba([normalize(text)])[0]
        order = np.argsort(-probs)
        ranking = [(self.classes[i], float(probs[i])) for i in order[:3]]
        return IntentPrediction(ranking[0][0], ranking[0][1], ranking)


# ---------------------------------------------------------------- slot extraction

_FUZZY_VOCAB = [
    "репозиторий", "репозитория", "песочница", "песочницу", "пространство", "пространству",
    "документация", "документации", "знаний", "чтение", "чтения", "редактирование",
    "редактирования", "запись", "записи", "продакшн", "доступ", "доступа", "заявку", "заявка",
]

_RESOURCE_PATTERNS: list[tuple[str, re.Pattern]] = [
    (
        "db_fintech_prod_cluster",
        re.compile(
            r"\bprod\b|\bпрод\b|прод[еа]?кшн|прод[еа]кшен|production|боев|промышлен|финтех|fintech|финанс|прод[ау]? кластер|прод бд"
        ),
    ),
    ("space_goods_search_wiki", re.compile(r"goods|поиск\w* товар|товарн\w* поиск")),
    (
        "repo_auto_core_sandbox",
        re.compile(r"репозитор|\bрепо\b|\bрепу\b|песочниц|sandbox|auto core|авто кор|\bgit\b|\bгит\b"),
    ),
    ("space_auto_backend_wiki", re.compile(r"auto backend|авто ?б[эе]кенд|автоб[эе]кенд")),
]
_GENERIC_WIKI = re.compile(
    r"вики|wiki|баз\w* знани|пространств|документаци|confluence|конфлюенс|материал\w* команд"
)

_WRITE_RE = re.compile(r"\bwrite\b|\bзапис[ьию]\b|на запись|редакт|права редактора|изменени|изменять|правк")
_READ_RE = re.compile(r"\bread\b|чтени|\bчита|просмотр|только смотреть")
_ADMIN_RE = re.compile(r"\badmin\b|админ")
_NEG_WRITE_RE = re.compile(r"\b(без|не нуж\w*|не надо|не требуется|не)\s+(\w+\s+)?(запис|редакт|write|правк)")
_NEG_READ_RE = re.compile(r"\b(не только|не просто)\s+(\w+\s+)?(чтени|чита|read)")
_ONLY_READ_RE = re.compile(r"только\s+(на\s+)?(чтени|чита|read|просмотр)")

_JUST_MARKER = re.compile(
    r"(?:^|[\s,.;:()—–-])(?P<m>для того чтобы|для того, чтобы|чтобы|потому что|так как|т\.\s?к\.|"
    r"обоснование\s*[:—–-]?|причина\s*[:—–-]?|цель\s*[:—–-]|зачем\s*[:—–-]|для)\s+(?P<rest>.+)$",
    re.S,
)
# Слова, которые после «для» описывают ресурс/уровень, а не обоснование.
_NON_REASON_WORDS = re.compile(
    r"^(записи|чтения|редактирования|read|write|меня|мне|нашей|моей|своей|команды|"
    r"auto|backend|goods|search|вики|wiki|репозитория|песочницы|пространства|базы|знаний|"
    r"доступа|доступ|к|в|на|и|admin|админа|кластера|прода)$"
)

_ACCESS_TARGET = re.compile(r"доступ\w*\s+(?:к|ко|в|во|до)\s+(?P<target>[\w\-\s]{2,40})")
_GENERIC_TARGETS = re.compile(r"^(нему|ней|нему же|этому|этой|ресурсу|ресурсам|системам|материалам)\b")


@dataclass
class Slots:
    resource_id: str | None = None
    access_level: str | None = None
    justification: str | None = None

    def to_dict(self) -> dict:
        return {
            "resource_id": self.resource_id,
            "access_level": self.access_level,
            "justification": self.justification,
        }

    @classmethod
    def from_dict(cls, data: dict | None) -> "Slots":
        data = data or {}
        return cls(
            resource_id=data.get("resource_id") or None,
            access_level=data.get("access_level") or None,
            justification=data.get("justification") or None,
        )

    def merged(self, other: "Slots") -> "Slots":
        return Slots(
            resource_id=other.resource_id or self.resource_id,
            access_level=other.access_level or self.access_level,
            justification=other.justification or self.justification,
        )

    def missing(self) -> list[str]:
        out = []
        if not self.resource_id:
            out.append("resource_id")
        if not self.access_level:
            out.append("access_level")
        if not self.justification or not (
            JUSTIFICATION_MIN <= len(self.justification) <= JUSTIFICATION_MAX
        ):
            out.append("justification")
        return out

    def any(self) -> bool:
        return bool(self.resource_id or self.access_level or self.justification)


def fuzzy_fix(text: str) -> str:
    """Исправляет опечатки в ключевых словах («репозитрий» → «репозиторий»)."""
    words = text.split(" ")
    fixed = []
    for w in words:
        core = re.sub(r"[^\w-]", "", w)
        if len(core) >= 6 and core not in _FUZZY_VOCAB:
            match = difflib.get_close_matches(core, _FUZZY_VOCAB, n=1, cutoff=0.84)
            if match:
                w = w.replace(core, match[0])
        fixed.append(w)
    return " ".join(fixed)


def _split_justification(text: str) -> tuple[str, str | None]:
    """Возвращает (часть-запрос, обоснование|None)."""
    for m in _JUST_MARKER.finditer(text):
        rest = m.group("rest").strip()
        words = re.findall(r"[\w-]+", rest)
        content = [w for w in words if not _NON_REASON_WORDS.match(w)]
        if m.group("m") == "для" and (len(content) < 2 or _NON_REASON_WORDS.match(words[0] if words else "")):
            continue
        reason = rest.strip(" .!;,?")
        if len(reason) < JUSTIFICATION_MIN:
            continue
        marker = m.group("m")
        if marker in ("для", "чтобы") or marker.startswith("для того"):
            reason = f"{marker} {reason}"  # «Для прохождения…» читается лучше, чем «Прохождения…»
        reason = reason[0].upper() + reason[1:]
        if len(reason) > JUSTIFICATION_MAX:
            reason = reason[:JUSTIFICATION_MAX].rsplit(" ", 1)[0]
        return text[: m.start()].strip(), reason
    return text, None


def extract_resource(text: str, user: UserContext) -> str | None:
    for rid in CATALOG:
        if rid in text:
            return rid
    for rid, pattern in _RESOURCE_PATTERNS:
        if pattern.search(text):
            return rid
    if _GENERIC_WIKI.search(text):
        if re.search(r"чуж\w*|соседн\w*|смежн\w*|друг\w* команд", text):
            return None  # неясно, какая именно команда — уточним
        own = [r for r in CATALOG.values() if r.kind == "wiki" and r.owner_team == user.team_id]
        return own[0].resource_id if own else None
    return None


def extract_level(text: str) -> str | None:
    if _ADMIN_RE.search(text):
        return "admin"
    write = bool(_WRITE_RE.search(text)) and not _NEG_WRITE_RE.search(text)
    read = bool(_READ_RE.search(text)) and not _NEG_READ_RE.search(text)
    if read and (_ONLY_READ_RE.search(text) or not write):
        return "read"
    if write:
        return "write"
    return None


def access_target(text: str) -> str | None:
    """Объект после «доступ к/в …» — для обнаружения ресурсов вне каталога (почта, VPN…)."""
    m = _ACCESS_TARGET.search(text)
    if not m:
        return None
    target = m.group("target").strip()
    target = re.split(r"\s(?:на|для|чтобы|с|и)\s", " " + target + " ")[0].strip() or target
    if _GENERIC_TARGETS.match(target):
        return None
    return target


def extract_slots(text: str, user: UserContext, expected: str | None = None) -> Slots:
    """Извлекает слоты из реплики. `expected` — какой параметр агент только что спросил."""
    norm = fuzzy_fix(normalize(text))
    request_part, reason = _split_justification(norm)
    # исходный регистр для обоснования
    if reason:
        idx = norm.find(reason.lower())
        if idx >= 0:
            original = re.sub(r"\s+", " ", text.strip())
            if len(original) == len(norm):
                reason = original[idx : idx + len(reason)]
                reason = reason[0].upper() + reason[1:]
    slots = Slots(
        resource_id=extract_resource(request_part, user) or extract_resource(norm, user),
        access_level=extract_level(request_part) or extract_level(norm),
        justification=reason,
    )
    if expected == "justification" and not slots.justification:
        candidate = re.sub(r"\s+", " ", text.strip()).strip(" .!")
        is_question = "?" in candidate or re.match(r"^(как|почему|зачем|кто|что|где|когда|сколько)\b", norm)
        if not is_question and not (slots.resource_id and slots.access_level):
            if len(candidate) >= JUSTIFICATION_MIN:
                slots.justification = candidate[0].upper() + candidate[1:JUSTIFICATION_MAX]
    return slots


def is_question(text: str) -> bool:
    norm = normalize(text)
    return "?" in text or bool(
        re.match(
            r"^(а |и |подскажи\w* |скажи\w* )?(как|какой|какая|какие|каков|почему|зачем|кто|что|где|когда|сколько|можно ли|могу ли|нужно ли|чем|куда|есть ли)\b",
            norm,
        )
    )
