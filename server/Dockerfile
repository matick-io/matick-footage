# Build context is the repository root: the image serves the web UI too.
FROM node:22-slim AS web
RUN corepack enable
WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.26 AS server
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /footage ./cmd/footage
# Mount points owned by the runtime user: new named volumes copy the owner.
RUN mkdir -p /mounts/library /mounts/data

FROM gcr.io/distroless/static-debian12
COPY --from=server /footage /usr/local/bin/footage
COPY --from=server --chown=1000:1000 /mounts/ /
COPY --from=web /web/dist /app/web
ENV FOOTAGE_WEB_DIR=/app/web
# The same user as the worker, so both can manage what the other wrote.
USER 1000:1000
EXPOSE 8080
ENTRYPOINT ["footage"]
CMD ["serve"]
