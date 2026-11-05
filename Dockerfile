FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG SITES=""
ARG TAGS=""
RUN go run ./internal/sites/all/gen.go
RUN set -eu; tags="$TAGS"; \
    if [ -n "$SITES" ]; then tags="explicit_sites $tags"; \
      for s in $(printf '%s' "$SITES" | tr ',' ' '); do tags="$tags site_$s"; done; \
    fi; \
    CGO_ENABLED=0 go build ${tags:+-tags "$tags"} -trimpath -ldflags "-s -w" -o /out/magpiesync ./cmd/magpiesync

FROM alpine:3.20
RUN apk add --no-cache ffmpeg ca-certificates tzdata \
    && adduser -D -u 10001 app \
    && mkdir -p /data \
    && chown app:app /data

COPY --from=build /out/magpiesync /usr/local/bin/magpiesync
USER app
WORKDIR /data
EXPOSE 8787
ENTRYPOINT ["magpiesync"]
CMD ["-listen", "0.0.0.0:8787", "-data-dir", "/data"]
