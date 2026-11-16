"""Клиент OpenAI-совместимого Chat Completions API с поддержкой function calling.

Подходит для OpenAI, YandexGPT (OpenAI-compatible endpoint), GigaChat через прокси,
vLLM, Ollama (`http://ollama:11434/v1`), LM Studio и т.п. Настраивается переменными
LLM_BASE_URL / LLM_API_KEY / LLM_MODEL / LLM_TIMEOUT.
"""

from __future__ import annotations

import json
from dataclasses import dataclass, field

import requests


class LLMError(RuntimeError):
    """Любая ошибка обращения к LLM: сеть, таймаут, HTTP, неожиданный формат."""


@dataclass
class ToolCall:
    name: str
    arguments: dict | None
    raw_arguments: str = ""


@dataclass
class LLMResponse:
    content: str = ""
    tool_calls: list[ToolCall] = field(default_factory=list)
    model: str = ""
    usage: dict = field(default_factory=dict)


class OpenAICompatibleClient:
    def __init__(self, base_url: str, api_key: str, model: str, timeout: float = 30.0, session=None):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.model = model
        self.timeout = timeout
        self.session = session or requests.Session()

    def chat(self, messages: list[dict], tools: list[dict] | None = None, temperature: float = 0.0) -> LLMResponse:
        payload: dict = {"model": self.model, "messages": messages, "temperature": temperature}
        if tools:
            payload["tools"] = tools
            payload["tool_choice"] = "auto"
        headers = {"Content-Type": "application/json"}
        if self.api_key:
            headers["Authorization"] = f"Bearer {self.api_key}"
        try:
            resp = self.session.post(
                f"{self.base_url}/chat/completions", json=payload, headers=headers, timeout=self.timeout
            )
        except requests.Timeout as exc:
            raise LLMError("llm timeout") from exc
        except requests.RequestException as exc:
            raise LLMError(f"llm transport error: {type(exc).__name__}") from exc
        if resp.status_code >= 400:
            raise LLMError(f"llm http {resp.status_code}")
        try:
            data = resp.json()
            message = data["choices"][0]["message"]
        except (ValueError, KeyError, IndexError, TypeError) as exc:
            raise LLMError("llm malformed response") from exc
        calls = []
        for call in message.get("tool_calls") or []:
            fn = call.get("function", {})
            raw = fn.get("arguments") or ""
            try:
                args = json.loads(raw) if isinstance(raw, str) else raw
            except json.JSONDecodeError:
                args = None
            calls.append(ToolCall(name=fn.get("name", ""), arguments=args, raw_arguments=str(raw)))
        return LLMResponse(
            content=message.get("content") or "",
            tool_calls=calls,
            model=data.get("model", self.model),
            usage=data.get("usage") or {},
        )
