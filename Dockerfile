# Build
FROM golang:1.25-bookworm AS builder

WORKDIR /src

# Dependências primeiro (cache de camadas)
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# Runtime
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /out/api ./api

ENV GIN_MODE=release
EXPOSE 8080

# Variáveis são lidas do ambiente (POSTGRES_URL, SERVER_PORT, DB_BACKENDS, etc.)
# Não inclua .env na imagem — passe com docker run -e ou compose env_file.
USER nobody:nogroup

ENTRYPOINT ["/app/api"]
