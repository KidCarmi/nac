# syntax=docker/dockerfile:1
#
# Multi-stage build for nacd. The final image is distroless and runs as a
# non-root user with a read-only root filesystem in the runtime manifests.

# ---- build stage ----
FROM golang:1.25 AS build
WORKDIR /src

# Cache module downloads independently of source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

# CGO disabled for a static binary; trimpath strips local paths for
# reproducibility. Build metadata is injected via ldflags.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w \
      -X github.com/kidcarmi/nac/pkg/version.Version=${VERSION} \
      -X github.com/kidcarmi/nac/pkg/version.Commit=${COMMIT} \
      -X github.com/kidcarmi/nac/pkg/version.BuildDate=${BUILD_DATE}" \
    -o /out/nacd ./cmd/nacd

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/nacd /nacd

# Non-root by default (distroless nonroot uid/gid 65532).
USER nonroot:nonroot
EXPOSE 8080

# Liveness is served at /healthz; orchestrators should probe it.
ENTRYPOINT ["/nacd"]
