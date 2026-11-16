"""Нормализация, токенизация и стемминг русского текста."""

from __future__ import annotations

import re
from functools import lru_cache

import snowballstemmer

_TOKEN_RE = re.compile(r"[a-zа-я0-9_]+", re.IGNORECASE)
_STEMMER_RU = snowballstemmer.stemmer("russian")
_STEMMER_EN = snowballstemmer.stemmer("english")

STOPWORDS = frozenset(
    """
    а и или но в во на по к ко с со из у от до для за о об без про через ли же бы
    не ни то это этот эта эти тот та те как так что чтобы где когда кто чем
    я мне меня мой моя мои моё мое мы нам нас наш наша наши ты тебе вы вам вас ваш
    он она оно они его ее её их им ему ей
    есть быть был была было были будет буду
    там тут здесь еще ещё уже вот только очень можно нужно надо
    пожалуйста привет здравствуйте подскажи подскажите скажи скажите
    расскажи расскажите объясни объясните такое такой такая какие какой какая каких каком
    изучи узнать хочу хотел хотела знать вообще
    the a an of to and or in on for is are be
    """.split()
)


def normalize(text: str) -> str:
    """Нижний регистр, ё→е, схлопывание пробелов."""
    text = (text or "").lower().replace("ё", "е")
    return re.sub(r"\s+", " ", text).strip()


def tokenize(text: str) -> list[str]:
    return _TOKEN_RE.findall(normalize(text))


@lru_cache(maxsize=50_000)
def stem(token: str) -> str:
    if re.search(r"[а-я]", token):
        return _STEMMER_RU.stemWord(token)
    if re.fullmatch(r"[a-z]+", token):
        return _STEMMER_EN.stemWord(token)
    return token


def stems(text: str, drop_stopwords: bool = True) -> list[str]:
    return [stem(t) for t in tokenize(text) if not (drop_stopwords and t in STOPWORDS)]


def content_tokens(text: str) -> set[str]:
    """Множество значимых стемов (для оценки перекрытия ответа с источником)."""
    return {s for s in stems(text) if len(s) > 2}
