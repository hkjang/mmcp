# syntax=docker/dockerfile:1
# mmcp — single offline image: React console + Go gateway on distroless.
FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY VERSION /src/VERSION
COPY web/ ./
RUN node node_modules/typescript/bin/tsc -b && node node_modules/vite/bin/vite.js build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN rm -rf internal/ui/dist
COPY --from=web /src/web/dist ./internal/ui/dist
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags="-s -w -X github.com/hkjang/mmcp/internal/buildinfo.Version=${VERSION} -X github.com/hkjang/mmcp/internal/buildinfo.Commit=${COMMIT} -X github.com/hkjang/mmcp/internal/buildinfo.BuildTime=${BUILD_TIME}" \
    -o /out/mmcp ./cmd/mmcp

FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
LABEL org.opencontainers.image.title="mmcp" \
      org.opencontainers.image.description="Mattermost MCP Gateway — Keycloak SSO, per-user Mattermost permissions, offline-ready" \
      org.opencontainers.image.source="https://github.com/hkjang/mmcp" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_TIME}"
COPY --from=go /out/mmcp /mmcp
COPY LICENSE THIRD_PARTY_NOTICES.md /usr/share/licenses/mmcp/
COPY web/public/fonts/OFL-Pretendard.txt /usr/share/licenses/mmcp/fonts/
USER nonroot:nonroot
EXPOSE 8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 CMD ["/mmcp", "healthcheck"]
ENTRYPOINT ["/mmcp"]
