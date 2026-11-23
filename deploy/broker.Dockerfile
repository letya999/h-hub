# credential-broker image: the only CGO binary in the project. The
# SQLCipher engine is built from a pinned source tarball and linked
# statically, so the runtime carries no sqlite/openssl shared libs.
# docker build -f deploy/broker.Dockerfile -t hhub-credential-broker:local .
FROM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build

ARG SQLCIPHER_VERSION=4.6.1
ARG SQLCIPHER_SHA256=d8f9afcbc2f4b55e316ca4ada4425daf3d0b4aab25f45e11a802ae422b9f53a3

WORKDIR /tmp
RUN set -eu; \
    apk add --no-cache gcc musl-dev make openssl-dev openssl-libs-static tcl; \
    wget -q "https://github.com/sqlcipher/sqlcipher/archive/v${SQLCIPHER_VERSION}.tar.gz" -O sqlcipher.tar.gz; \
    test "$(sha256sum sqlcipher.tar.gz | cut -d' ' -f1)" = "${SQLCIPHER_SHA256}"; \
    tar -xzf sqlcipher.tar.gz; \
    cd "sqlcipher-${SQLCIPHER_VERSION}"; \
    ./configure --disable-shared --enable-static --enable-tempstore=yes \
        --with-crypto-lib=openssl CFLAGS="-DSQLITE_HAS_CODEC -O2"; \
    make -j"$(nproc)" libsqlcipher.la; \
    mkdir -p /sqlcipher/lib /sqlcipher/include; \
    cp .libs/libsqlcipher.a /sqlcipher/lib/libsqlite3.a; \
    cp sqlite3.h /sqlcipher/include/sqlite3.h

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 \
    CGO_CFLAGS="-I/sqlcipher/include" \
    CGO_LDFLAGS="-L/sqlcipher/lib -static" \
    go build -tags libsqlite3 -trimpath -o /out/svc \
    ./services/credential-broker

FROM alpine:3.21@sha256:bc21487101ae47172509130ee1092cc273a831968d64b617776d1f69c81e42ab
COPY --from=build /out/svc /svc
EXPOSE 9000
ENTRYPOINT ["/svc"]
