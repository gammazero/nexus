FROM docker.io/library/golang:latest AS build-image

ARG BUILD_VERSION="latest"

RUN go install github.com/gammazero/nexus/v3/nexusd@${BUILD_VERSION}

FROM docker.io/library/busybox:latest AS release-image

ARG BUILD_VERSION="latest"

COPY --from=build-image /go/bin/nexusd /usr/bin/nexusd

EXPOSE 8080

ENTRYPOINT ["/usr/bin/nexusd"]
CMD ["--ws", "0.0.0.0:8080", "--realm", "realm1"]

ENV TZ=Etc/UTC

LABEL name="nexusd"
LABEL version="${BUILD_VERSION}"
LABEL vendor="Nexus WAMP Contributors"
LABEL description="Nexus is a WAMP v2 router library, client library, and a router \
service, that implements most of the features defined in the WAMP-protocol advanced profile"
