# syntax=docker/dockerfile:1
# The runner image (spec 0001). Build context is this folder:
#   docker build -f services/runner/Dockerfile services/runner
# Base images match ModularPlatform's pins (catalog/modules/renderer/Dockerfile).

FROM golang:1.27.3-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath GOWORK=off
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -o /out/runner ./cmd/runner && \
    mkdir -p /out/run-logs

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/runner /app/runner
# a fresh run-logs volume copies this folder, so it starts owned by nonroot (65532)
COPY --from=build --chown=65532:65532 /out/run-logs /var/lib/adp/run-logs
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/runner"]
