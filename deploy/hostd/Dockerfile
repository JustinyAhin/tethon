# syntax=docker/dockerfile:1

FROM golang:1.26-bookworm AS build

WORKDIR /src/control
COPY control/go.mod control/go.sum ./
RUN go mod download

COPY control/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/tethon-hostd ./cmd/tethon-hostd

FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive
ENV DISABLE_AUTOUPDATER=1
ENV HOME=/var/lib/tethon/home
ENV PATH=/usr/local/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates curl git \
	&& install -d -m 0755 /etc/apt/keyrings \
	&& curl -fsSL https://downloads.claude.ai/keys/claude-code.asc -o /etc/apt/keyrings/claude-code.asc \
	&& chmod 0644 /etc/apt/keyrings/claude-code.asc \
	&& echo "deb [signed-by=/etc/apt/keyrings/claude-code.asc] https://downloads.claude.ai/claude-code/apt/stable stable main" > /etc/apt/sources.list.d/claude-code.list \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends claude-code \
	&& rm -rf /var/lib/apt/lists/*

COPY --from=golang:1.26-bookworm /usr/local/go /usr/local/go
COPY --from=oven/bun:1 /usr/local/bin/bun /usr/local/bin/bun
RUN ln -s /usr/local/bin/bun /usr/local/bin/bunx

RUN groupadd --gid 10001 tethon \
	&& useradd --uid 10001 --gid 10001 --home-dir /var/lib/tethon/home --shell /bin/bash tethon \
	&& mkdir -p /var/lib/tethon/home /var/lib/tethon/repos /var/lib/tethon/worktrees /var/lib/tethon/cache \
	&& chown -R tethon:tethon /var/lib/tethon

COPY --from=build /out/tethon-hostd /usr/local/bin/tethon-hostd
COPY --chmod=0755 deploy/hostd/entrypoint.sh /usr/local/bin/tethon-hostd-entrypoint

USER tethon:tethon

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
	CMD curl -fsS http://127.0.0.1:39181/healthz >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/tethon-hostd-entrypoint"]
