# Reproducible scanner environment; never part of the production image.
FROM node:26-alpine AS node
RUN npm install --global pnpm@12.16.1
FROM golang:1.27.3-alpine
RUN apk add --no-cache libstdc++
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules/pnpm /usr/local/lib/node_modules/pnpm
RUN ln -s /usr/local/lib/node_modules/pnpm/pnpm /usr/local/bin/pnpm && pnpm --version
RUN go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY web/ web/
COPY resources/ resources/
COPY desktop/package.json desktop/pnpm-lock.yaml desktop/pnpm-workspace.yaml desktop/
COPY desktop/palette.css desktop/
COPY scripts/audit-desktop.mjs scripts/
COPY tests/security/ tests/security/
RUN pnpm --dir web install --frozen-lockfile --ignore-scripts && pnpm --dir web run build
RUN pnpm --dir web audit --audit-level=low && node --test tests/security/desktop-audit.test.mjs && node scripts/audit-desktop.mjs && govulncheck ./cmd/... ./internal/...
