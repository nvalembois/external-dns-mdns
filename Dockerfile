ARG GO_VERSION=1.25.12@sha256:d7912cedddfa15b2900a8dfb7187df0af5ec2cb424a371139b5b352fd3e6b740
FROM golang:${GO_VERSION} AS build

ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum /go/src/github.com/nvalembois/external-dns-mdns-server/
COPY cmd /go/src/github.com/nvalembois/external-dns-mdns-server/cmd
COPY pkg /go/src/github.com/nvalembois/external-dns-mdns-server/pkg
WORKDIR /go/src/github.com/nvalembois/external-dns-mdns-server

RUN ls && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build \
    -trimpath -ldflags="-s -w" \
    -o external-dns-mdns-server cmd/external-dns-mdns-server.go

RUN echo 'external-dns-mdns-server:x:10001:10001:External DNS MDNs Server Daemon:/:/usr/bin/nologin' >password

FROM scratch
COPY --from=build --chown=1:1 --chmod=0755 /go/src/github.com/nvalembois/external-dns-mdns-server/external-dns-mdns-server /external-dns-mdns-server
COPY --from=build --chown=0:0 --chmod=0644 /go/src/github.com/nvalembois/external-dns-mdns-server/password /etc/password
COPY --from=build /etc/ssl/certs /etc/ssl/certs
USER external-dns-mdns-server
ENTRYPOINT ["/external-dns-mdns-server"]
