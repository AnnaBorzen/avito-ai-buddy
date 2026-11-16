# ML-сервис AI-buddy: RAG с ACL + NLU + guardrails + агент (Flask/gunicorn, CPU).
FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

WORKDIR /app

COPY requirements.txt .
RUN pip install -r requirements.txt

# Опционально: dense-эмбеддинги (docker compose build --build-arg WITH_DENSE=1 ml)
ARG WITH_DENSE=0
COPY requirements-dense.txt .
RUN if [ "$WITH_DENSE" = "1" ]; then \
      pip install -r requirements-dense.txt --extra-index-url https://download.pytorch.org/whl/cpu ; \
    fi

COPY buddy_ml ./buddy_ml
COPY knowledge ./knowledge
COPY scripts ./scripts

# Открытый AvitoTech Playbook (зафиксированный коммит). Без сети сборка не падает —
# сервис работает на тестовых инструкциях и чек-листе.
ARG WITH_PLAYBOOK=1
RUN if [ "$WITH_PLAYBOOK" = "1" ]; then python scripts/fetch_playbook.py /app/data/external/playbook ; fi

RUN useradd --create-home --uid 10001 buddy && chown -R buddy /app
USER buddy

ENV ML_PORT=8090 \
    ML_PLAYBOOK_DIR=/app/data/external/playbook
EXPOSE 8090

HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=5 \
  CMD python -c "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:8090/healthz', timeout=2).status == 200 else 1)"

# Индекс и классификатор строятся при старте каждого воркера (~1–2 с); --preload строит их один раз.
CMD ["gunicorn", "--bind", "0.0.0.0:8090", "--workers", "2", "--threads", "4", "--timeout", "60", "--preload", "buddy_ml.wsgi:app"]
