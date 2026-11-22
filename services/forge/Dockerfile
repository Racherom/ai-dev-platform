# syntax=docker/dockerfile:1
# The forge image (spec 0001): the forge service and its one shot migrate binary. The build context is
# the repo root, so the image can include libs/ through the replace in go.mod:
#   docker build -f services/forge/Dockerfile .
# Base images match ModularPlatform's pins (catalog/modules/renderer/Dockerfile).

FROM golang:1.27.3-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build
ENV CGO_ENABLED=0 GOFLAGS=-trimpath GOWORK=off
WORKDIR /src/services/forge
COPY libs/go.mod libs/go.sum /src/libs/
COPY services/forge/go.mod services/forge/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY libs/ /src/libs/
COPY services/forge/ ./
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -o /out/forge ./cmd/forge && \
    go build -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/forge /out/migrate /app/
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/forge"]
