FROM golang:1.25.5-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/voie .

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates chromium \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --create-home --uid 10001 voie \
    && mkdir -p /data \
    && chown voie:voie /data

COPY --from=build /out/voie /usr/local/bin/voie

ENV HOST=0.0.0.0 \
    PORT=8080 \
    CONVERSATION_DB_PATH=/data/conversations.db

EXPOSE 8080
VOLUME ["/data"]

USER voie
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 CMD ["/usr/local/bin/voie", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/voie", "serve"]
