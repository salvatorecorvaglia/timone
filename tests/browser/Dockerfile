# Browser acceptance only; Chromium runs in an isolated test container.
FROM timone:ci AS app
FROM node:24-alpine
RUN apk add --no-cache chromium
WORKDIR /work/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN corepack enable && corepack prepare pnpm@12.16.1 --activate
RUN pnpm install --frozen-lockfile --ignore-scripts
COPY --from=app /usr/local/bin/timone /work/bin/timone
COPY tests/browser /work/tests/browser
ENV TIMONE_BROWSER_EXECUTABLE=/usr/bin/chromium TIMONE_BROWSER_ARTIFACTS=/work/artifacts
WORKDIR /work
CMD ["node", "tests/browser/run.mjs"]
