# syntax=docker/dockerfile:1

# Bin Tracker server image.
#   docker build -t bintracker .
#   docker run -d -p 8420:8420 -p 8421:8421 -v bintracker-data:/data bintracker
# Multi-architecture (e.g. for a Raspberry Pi or ARM NAS):
#   docker buildx build --platform linux/amd64,linux/arm64 -t <registry>/bintracker:1.0.0 --push .

# ---- build: cross-compiles on the build machine for the target platform ----
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w" -o /out/bintracker . \
 && mkdir -p /out/data

# ---- run: minimal image with no shell, running as a non-root user ----
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/bintracker /bintracker
COPY --from=build --chown=65532:65532 /out/data /data

ENV BINTRACKER_SERVER=true \
    BINTRACKER_DATA=/data \
    BINTRACKER_PORT=8420

VOLUME /data
# 8420: web app (HTTP). 8421: HTTPS, needed for phone camera scanning.
EXPOSE 8420 8421

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/bintracker", "-healthcheck"]

ENTRYPOINT ["/bintracker"]
