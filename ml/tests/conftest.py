from __future__ import annotations

import sys
from pathlib import Path

import pytest

ML_DIR = Path(__file__).resolve().parents[1]
ROOT = ML_DIR.parent
sys.path.insert(0, str(ML_DIR))
sys.path.insert(0, str(ML_DIR / "scripts"))

from buddy_ml.agent import Agent, TurnRequest  # noqa: E402
from buddy_ml.app import build_agent  # noqa: E402
from buddy_ml.config import DEFAULT_PLAYBOOK_DIR, Settings  # noqa: E402
from buddy_ml.evaluation import user_dict  # noqa: E402

PLAYBOOK_AVAILABLE = DEFAULT_PLAYBOOK_DIR.exists() and any(DEFAULT_PLAYBOOK_DIR.glob("*.md"))


def make_settings(playbook: bool = False, **overrides) -> Settings:
    s = Settings()
    s.playbook_dir = DEFAULT_PLAYBOOK_DIR if playbook else None
    for k, v in overrides.items():
        setattr(s, k, v)
    return s


@pytest.fixture(scope="session")
def agent() -> Agent:
    """Агент на тестовых инструкциях + чек-листе (без Playbook — детерминированно и быстро)."""
    return build_agent(make_settings(playbook=False))


@pytest.fixture(scope="session")
def full_agent() -> Agent:
    """Агент со всей базой знаний, включая Playbook (если скачан)."""
    return build_agent(make_settings(playbook=PLAYBOOK_AVAILABLE))


@pytest.fixture
def intern1() -> dict:
    return user_dict("usr_intern_01")


@pytest.fixture
def intern2() -> dict:
    return user_dict("usr_intern_02")


@pytest.fixture
def lead() -> dict:
    return user_dict("usr_lead_01")


@pytest.fixture
def ask(agent, intern1):
    def _ask(message: str, user: dict | None = None, a: Agent | None = None, **body) -> dict:
        payload = {"user": user or intern1, "message": message, **body}
        return (a or agent).turn(TurnRequest.from_dict(payload))

    return _ask
