# syntax=docker/dockerfile:1

FROM golang:1.26-bookworm AS build

WORKDIR /src/control
COPY control/go.mod control/go.sum ./
RUN go mod download

COPY control/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/tethon-hub ./cmd/tethon-hub

FROM alpine:3.22

RUN apk add --no-cache ca-certificates

COPY --from=build /out/tethon-hub /usr/local/bin/tethon-hub

USER nobody:nobody
EXPOSE 39180

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
	CMD wget -qO- http://127.0.0.1:39180/healthz >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/tethon-hub"]
CMD ["-addr", "0.0.0.0:39180"]
