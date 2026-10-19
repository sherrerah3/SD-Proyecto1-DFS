# syntax=docker/dockerfile:1
ARG GO_VERSION=1.27.1

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine3.24 AS builder
WORKDIR /src

RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN mkdir -p /out && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags="-s -w" -o /out/dfsha-metanode ./cmd/metanode && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags="-s -w" -o /out/dfsha-datanode ./cmd/datanode && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags="-s -w" -o /out/dfsha-client ./cmd/client

FROM alpine:3.24.2 AS runtime
RUN apk add --no-cache ca-certificates tzdata nano \
    && addgroup -S dfsha \
    && adduser -S -D -h /home/dfsha -s /bin/sh -G dfsha dfsha \
    && mkdir -p /var/lib/dfsha/metanode/MetaNode-1 /var/lib/dfsha/blocks /app/logs/client /app/test-files /home/dfsha \
    && chown -R dfsha:dfsha /var/lib/dfsha /app /home/dfsha

WORKDIR /app
COPY --from=builder /out/dfsha-metanode /usr/local/bin/dfsha-metanode
COPY --from=builder /out/dfsha-datanode /usr/local/bin/dfsha-datanode
COPY --from=builder /out/dfsha-client /usr/local/bin/dfsha-client
COPY --chmod=755 docker/entrypoint.sh /usr/local/bin/dfsha-entrypoint
COPY --chown=dfsha:dfsha test-files/hello.txt /app/test-files/hello.txt
COPY --chown=dfsha:dfsha test-files/images.jpg /app/test-files/images.jpg
COPY --chown=dfsha:dfsha test-files/sample-5s.mp4 /app/test-files/sample-5s.mp4
COPY --chown=dfsha:dfsha test-files/wow.txt /app/test-files/wow.txt

USER dfsha:dfsha
ENV HOME=/home/dfsha \
    EDITOR=nano \
    VISUAL=nano \
    TERM=xterm-256color
ENTRYPOINT ["/usr/local/bin/dfsha-entrypoint"]
CMD ["metanode"]
