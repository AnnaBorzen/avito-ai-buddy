FROM golang:1.26.8-alpine AS builder

WORKDIR /app

# В проекте пока нет внешних зависимостей, поэтому go.sum отсутствует:
# копируем только go.mod (шаблон COPY go.sum* падал бы на пустом списке файлов).
COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOTOOLCHAIN=local \
    go build -trimpath -ldflags="-s -w" -o /app/server ./cmd/api

# Runtime
FROM alpine:3.22

RUN addgroup -S app && adduser -S -G app app

WORKDIR /app

COPY --from=builder /app/server ./server

USER app

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 \
    CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

CMD ["./server"]
