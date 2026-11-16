FROM node:22-alpine

# The build context is the repository root: the demo API (server/assistant.mjs)
# reads ../../rag_docs, so frontend and rag_docs must keep sibling layout.
WORKDIR /app/frontend

# package-lock.json обязателен: npm ci даёт воспроизводимую установку.
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

COPY frontend/ ./
COPY rag_docs /app/rag_docs

EXPOSE 5173

# Vite по умолчанию слушает localhost; в контейнере нужен внешний интерфейс,
# иначе опубликованный порт недоступен с хоста.
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]

