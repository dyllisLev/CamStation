# syntax=docker/dockerfile:1.7

FROM node:22.22.1-alpine3.23@sha256:8094c002d08262dba12645a3b4a15cd6cd627d30bc782f53229a2ec13ee22a00 AS web-builder

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.25.12-alpine3.23@sha256:cc985ef6f9c3bf9ece7488129c9abe0a150388ccdfa428d886fc709dca0b230a AS go-builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ ./internal/
COPY cmd/camstationd/ ./cmd/camstationd/
COPY cmd/camstation-migrate/ ./cmd/camstation-migrate/
COPY --from=web-builder /src/cmd/camstationd/web/ ./cmd/camstationd/web/
RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
      go build -trimpath -buildvcs=false -o /out/camstationd ./cmd/camstationd && \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
      go build -trimpath -buildvcs=false -o /out/camstation-migrate ./cmd/camstation-migrate

FROM ghcr.io/alexxit/go2rtc:1.9.14@sha256:675c318b23c06fd862a61d262240c9a63436b4050d177ffc68a32710d9e05bae AS go2rtc-binary

FROM alpine:3.23.2@sha256:865b95f46d98cf867a156fe4a135ad3fe50d2056aa3f25ed31662dff6da4eb62

ARG VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.title="CamStation" \
      org.opencontainers.image.description="CamStation 2.0 CCTV/NVR application runtime" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.source="https://github.com/dyllisLev/CamStation" \
      org.opencontainers.image.url="https://github.com/dyllisLev/CamStation" \
      org.opencontainers.image.licenses="NOASSERTION" \
      org.opencontainers.image.base.name="alpine:3.23.2@sha256:865b95f46d98cf867a156fe4a135ad3fe50d2056aa3f25ed31662dff6da4eb62" \
      io.camstation.go2rtc.image="ghcr.io/alexxit/go2rtc:1.9.14@sha256:675c318b23c06fd862a61d262240c9a63436b4050d177ffc68a32710d9e05bae" \
      io.camstation.go2rtc.binary.sha256="60384f150325733f156245caa23c60ee9f9e176f666fe50df5f213b238419b2d"

RUN apk add --no-cache \
      ca-certificates=20260611-r0 \
      ffmpeg=8.0.1-r1 \
      rclone=1.72.1-r4 \
      tini=0.19.0-r3 \
      tzdata=2026c-r0 && \
    addgroup -S -g 10001 camstation && \
    adduser -S -D -H -u 10001 -G camstation camstation && \
    install -d -o camstation -g camstation -m 0750 \
      /var/lib/camstation \
      /var/lib/camstation/data \
      /var/lib/camstation/media \
      /var/lib/camstation/media/recordings \
      /var/lib/camstation/media/temp && \
    test -x /usr/bin/ffmpeg && \
    test -x /usr/bin/ffprobe && \
    test -x /usr/bin/rclone && \
    test -x /sbin/tini

COPY --from=go2rtc-binary --chown=root:root /usr/local/bin/go2rtc /usr/local/bin/go2rtc
COPY --from=go-builder --chown=root:root /out/camstationd /usr/local/bin/camstationd
COPY --from=go-builder --chown=root:root /out/camstation-migrate /usr/local/bin/camstation-migrate
RUN echo "60384f150325733f156245caa23c60ee9f9e176f666fe50df5f213b238419b2d  /usr/local/bin/go2rtc" | sha256sum -c -

WORKDIR /var/lib/camstation
ENV HOME=/var/lib/camstation \
    TMPDIR=/tmp \
    XDG_CONFIG_HOME=/var/lib/camstation/data/xdg-config \
    XDG_CACHE_HOME=/var/lib/camstation/data/xdg-cache \
    CAMSTATION_ADDR=0.0.0.0:18080 \
    CAMSTATION_DB=/var/lib/camstation/data/camstation.db \
    CAMSTATION_RECORDINGS_DIR=/var/lib/camstation/media/recordings \
    CAMSTATION_TEMP_DIR=/var/lib/camstation/media/temp \
    CAMSTATION_VIEWER_RELEASES_DIR=/var/lib/camstation/data/viewer-releases \
    CAMSTATION_RECORDING_ENABLED=false

EXPOSE 18080/tcp 8555/tcp 8555/udp
HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:18080/api/health || exit 1

USER 10001:10001
ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/usr/local/bin/camstationd"]
