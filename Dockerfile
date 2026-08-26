# syntax=docker/dockerfile:1.7

FROM node:24-alpine AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -mod=readonly \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app
COPY --from=go-build --chown=nonroot:nonroot /out/server /app/server
COPY --from=web-build --chown=nonroot:nonroot /src/web/dist /app/web

ENV APP_ENV=production \
    APP_LISTEN_ADDR=:8080 \
    APP_WEB_DIR=/app/web

USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD ["/app/server", "healthcheck", "--url", "http://127.0.0.1:8080/healthz"]
ENTRYPOINT ["/app/server"]
CMD ["serve"]
