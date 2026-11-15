"""Каталог ресурсов, синтетические профили и консультативная RBAC-проверка.

ВАЖНО: это не замена проверки прав на backend. Агент использует политику, чтобы
заранее объяснить отказ и не предлагать заведомо запрещённый черновик; backend
повторно проверяет права при подтверждении (FR-09, AC-02).

Источник истины — поле `permissions` в запросе от backend. Встроенная матрица
(раздел 7 ТЗ) используется, только если backend её не передал (автономный режим, evals).
"""

from __future__ import annotations

from dataclasses import dataclass, field

TOOL_LEVELS = ("read", "write")  # enum access_level в propose_access_request


@dataclass(frozen=True)
class Resource:
    resource_id: str
    title: str
    owner_team: str
    kind: str  # wiki | repo | prod_db
    levels: tuple[str, ...]


CATALOG: dict[str, Resource] = {
    r.resource_id: r
    for r in (
        Resource(
            "space_auto_backend_wiki",
            "Пространство базы знаний и документации Auto Backend",
            "team_auto_backend",
            "wiki",
            ("read", "write"),
        ),
        Resource(
            "repo_auto_core_sandbox",
            "Учебный Git-репозиторий песочницы Auto Core",
            "team_auto_backend",
            "repo",
            ("read", "write"),
        ),
        Resource(
            "space_goods_search_wiki",
            "Пространство базы знаний команды Поиска Товаров",
            "team_goods_search",
            "wiki",
            ("read",),
        ),
        Resource(
            "db_fintech_prod_cluster",
            "Боевой кластер БД Финансовых сервисов (Prod)",
            "team_fintech_core",
            "prod_db",
            ("read", "admin"),
        ),
    )
}

TEAM_NAMES = {
    "team_auto_backend": "Auto Backend",
    "team_goods_search": "Поиск Товаров",
    "team_fintech_core": "Fintech Core",
}


@dataclass(frozen=True)
class UserContext:
    """Доверенный серверный контекст. Никогда не берётся из текста сообщения."""

    user_id: str
    name: str
    role: str
    team_id: str
    team_name: str = ""
    mentor_handle: str = ""
    approver_name: str = ""

    @classmethod
    def from_dict(cls, data: dict) -> "UserContext":
        team_id = str(data["team_id"])
        return cls(
            user_id=str(data["user_id"]),
            name=str(data.get("name", "")),
            role=str(data["role"]),
            team_id=team_id,
            team_name=str(data.get("team_name") or TEAM_NAMES.get(team_id, team_id)),
            mentor_handle=str(data.get("mentor_handle") or ""),
            approver_name=str(data.get("approver_name") or ""),
        )


SEED_USERS: dict[str, UserContext] = {
    u.user_id: u
    for u in (
        UserContext(
            "usr_intern_01", "Алексей Смирнов", "intern", "team_auto_backend",
            "Auto Backend", "@ivan_petrov", "Дмитрий Волков (Team Lead)",
        ),
        UserContext(
            "usr_intern_02", "Мария Иванова", "intern", "team_goods_search",
            "Поиск Товаров", "@olga_sidorova", "Team Lead Поиска Товаров",
        ),
        UserContext(
            "usr_lead_01", "Дмитрий Волков", "lead", "team_auto_backend",
            "Auto Backend", "", "",
        ),
    )
}


@dataclass
class Permissions:
    """resource_id -> разрешённые уровни для текущего пользователя."""

    allowed: dict[str, tuple[str, ...]] = field(default_factory=dict)

    @classmethod
    def from_request(cls, items: list[dict] | None, user: UserContext) -> "Permissions":
        if not items:
            return default_permissions(user)
        allowed = {}
        for item in items:
            rid = str(item["resource_id"])
            allowed[rid] = tuple(str(x) for x in item.get("allowed_levels", []))
        return cls(allowed)

    def levels(self, resource_id: str) -> tuple[str, ...]:
        return self.allowed.get(resource_id, ())


def default_permissions(user: UserContext) -> Permissions:
    """Матрица из раздела 7.2 ТЗ, обобщённая на любую команду стажёра."""
    allowed: dict[str, tuple[str, ...]] = {}
    for r in CATALOG.values():
        if user.role == "intern":
            if r.kind == "prod_db":
                levels: tuple[str, ...] = ()
            elif r.owner_team == user.team_id:
                levels = r.levels
            elif r.kind == "wiki":
                levels = tuple(lv for lv in r.levels if lv == "read")
            else:
                levels = ()  # чужие учебные репозитории стажёру не положены
        elif user.role in ("lead", "mentor"):
            levels = r.levels if r.kind != "prod_db" else ()
        else:
            levels = ()
        allowed[r.resource_id] = levels
    return Permissions(allowed)


@dataclass(frozen=True)
class PolicyDecision:
    allowed: bool
    code: str  # OK | RBAC_ACCESS_DENIED | RBAC_LEVEL_NOT_ALLOWED | RESOURCE_UNKNOWN | LEVEL_UNSUPPORTED
    message: str
    allowed_levels: tuple[str, ...] = ()

    def to_dict(self) -> dict:
        return {
            "allowed": self.allowed,
            "code": self.code,
            "message": self.message,
            "allowed_levels": list(self.allowed_levels),
        }


_LEVEL_RU = {"read": "чтение (read)", "write": "редактирование (write)", "admin": "администрирование (admin)"}


def level_ru(level: str) -> str:
    return _LEVEL_RU.get(level, level)


def check(user: UserContext, perms: Permissions, resource_id: str, level: str | None) -> PolicyDecision:
    resource = CATALOG.get(resource_id)
    if resource is None:
        return PolicyDecision(False, "RESOURCE_UNKNOWN", "Такого ресурса нет в каталоге.")
    allowed = perms.levels(resource_id)
    if not allowed:
        return PolicyDecision(
            False,
            "RBAC_ACCESS_DENIED",
            f"Роли «{user.role}» недоступен запрос к ресурсу «{resource.title}».",
        )
    if level is None:
        return PolicyDecision(True, "OK", "", allowed)
    if level not in TOOL_LEVELS:
        return PolicyDecision(
            False,
            "LEVEL_UNSUPPORTED",
            f"Уровень «{level}» нельзя запросить через помощника: доступны только read и write.",
            allowed,
        )
    if level not in allowed:
        return PolicyDecision(
            False,
            "RBAC_LEVEL_NOT_ALLOWED",
            f"Для ресурса «{resource.title}» вам доступен только уровень "
            + ", ".join(level_ru(lv) for lv in allowed)
            + f"; {level_ru(level)} запросить нельзя.",
            allowed,
        )
    return PolicyDecision(True, "OK", "", allowed)
