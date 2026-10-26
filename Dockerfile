# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X github.com/BorisGB2001/RuConnect/internal/api.Version=${VERSION}" \
      -o /out/ruconnect ./cmd/ruconnect \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ruconnect /usr/local/bin/ruconnect
# Owned by the nonroot user so a fresh named volume is writable.
COPY --from=build --chown=65532:65532 /out/data /data
ENV RUCONNECT_LISTEN=:21114 \
    RUCONNECT_DB=/data/ruconnect.db \
    RUCONNECT_KEY_FILE=/hbbs/id_ed25519.pub
VOLUME /data
EXPOSE 21114
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/usr/local/bin/ruconnect", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/ruconnect"]
