# Build
FROM golang:alpine AS build

RUN apk add --no-cache -U build-base git make

RUN mkdir -p /src

WORKDIR /src

# Copy Makefile
COPY Makefile ./

# Install deps
RUN make deps

# Copy go.mod and go.sum and install and cache dependencies
COPY go.mod .
COPY go.sum .

# Copy sources
COPY *.go ./
COPY ./bits/*.go ./bits/
COPY ./fs/*.go ./fs/
COPY ./message/*.go ./message/
COPY ./metadata/client/*.go ./metadata/client/
COPY ./metadata/server/*.go ./metadata/server/
COPY ./storage/*.go ./storage/
COPY ./cmd/CryptFS/*.go ./cmd/CryptFS/

# Version/Commit (there there is no .git in Docker build context)
# NOTE: This is fairly low down in the Dockerfile instructions so
#       we don't break the Docker build cache just be changing
#       unrelated files that actually haven't changed but caused the
#       COMMIT value to change.
ARG VERSION="0.0.0"
ARG COMMIT="HEAD"

# Build cli binary
RUN make cli VERSION=$VERSION COMMIT=$COMMIT

# Runtime
FROM alpine:latest

RUN apk --no-cache -U add su-exec shadow ca-certificates tzdata

ENV PUID=1000
ENV PGID=1000

RUN addgroup -g "${PGID}" CryptFS && \
    adduser -D -H -G CryptFS -h /var/empty -u "${PUID}" CryptFS && \
    mkdir -p /data && chown -R CryptFS:CryptFS /data

VOLUME /data

WORKDIR /

# force cgo resolver
ENV GODEBUG=netdns=cgo

COPY --from=build /src/CryptFS /usr/local/bin/CryptFS

COPY .dockerfiles/entrypoint.sh /init

ENTRYPOINT ["/init"]
CMD ["CryptFS"]
