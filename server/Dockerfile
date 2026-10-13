# Build context is the repository root, so the image can include the built UI.
FROM golang:1.26 AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /footage ./cmd/footage

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /footage /usr/local/bin/footage
# Until matick-ui is on npm, web/dist is built on the host first (make web).
COPY web/dist /app/web
ENV FOOTAGE_WEB_DIR=/app/web
EXPOSE 8080
ENTRYPOINT ["footage"]
CMD ["serve"]
