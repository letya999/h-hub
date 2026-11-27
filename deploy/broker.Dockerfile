# credential-broker image: the only CGO binary in the project. The
# SQLCipher engine comes from the pinned GHCR base
# (deploy/sqlcipher-base.ref) and links statically, so the runtime
# carries no sqlite/openssl shared libs.
# docker build --build-arg SQLCIPHER_BASE=$(cat deploy/sqlcipher-base.ref) \
#   -f deploy/broker.Dockerfile -t hhub-credential-broker:local .
ARG SQLCIPHER_BASE
FROM ${SQLCIPHER_BASE} AS sqlcipher

FROM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build

RUN apk add --no-cache gcc musl-dev openssl-dev openssl-libs-static
RUN mkdir -p /sqlcipher/lib /sqlcipher/include
COPY --from=sqlcipher /lib/libsqlite3.a /sqlcipher/lib/libsqlite3.a
COPY --from=sqlcipher /include/sqlite3.h /sqlcipher/include/sqlite3.h

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 \
    CGO_CFLAGS="-I/sqlcipher/include" \
    CGO_LDFLAGS="-L/sqlcipher/lib -static" \
    go build -tags libsqlite3 -trimpath -o /out/svc \
    ./services/credential-broker

FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
COPY --from=build /out/svc /svc
EXPOSE 9000
ENTRYPOINT ["/svc"]
