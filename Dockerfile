FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN corepack enable && corepack prepare pnpm@11.23.0 --activate
RUN pnpm install --frozen-lockfile --ignore-scripts
COPY web/ ./
RUN pnpm run build

FROM --platform=$BUILDPLATFORM golang:1.26.10-alpine AS backend
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY web/embed.go web/package.json web/
COPY --from=frontend /src/web/dist web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /timone ./cmd/timone

FROM alpine:3.23
LABEL org.opencontainers.image.source="https://github.com/salvatorecorvaglia/timone" \
      org.opencontainers.image.description="Read-only Kubernetes insights and diagnostics" \
      org.opencontainers.image.licenses="MIT"
RUN apk add --no-cache ca-certificates && addgroup -g 10001 timone && adduser -D -H -u 10001 -G timone timone
COPY --from=backend /timone /usr/local/bin/timone
USER 10001:10001
ENV TIMONE_MODE=incluster TIMONE_LISTEN=0.0.0.0:8080
EXPOSE 8080
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/timone"]
