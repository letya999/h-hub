# sqlcipher-base: pinned SQLCipher static library published to GHCR.
# CI builds this image once (.github/workflows/sqlcipher.yml); consumers
# pull it by the digest pinned in deploy/sqlcipher-base.ref and COPY the
# artifacts instead of compiling SQLCipher themselves.
FROM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build

ARG SQLCIPHER_VERSION=4.6.1
ARG SQLCIPHER_SHA256=d8f9afcbc2f4b55e316ca4ada4425daf3d0b4aab25f45e11a802ae422b9f53a3

RUN set -eu; \
    apk add --no-cache gcc musl-dev make openssl-dev openssl-libs-static tcl; \
    cd /tmp; \
    wget -q "https://github.com/sqlcipher/sqlcipher/archive/v${SQLCIPHER_VERSION}.tar.gz" -O sqlcipher.tar.gz; \
    test "$(sha256sum sqlcipher.tar.gz | cut -d' ' -f1)" = "${SQLCIPHER_SHA256}"; \
    tar -xzf sqlcipher.tar.gz; \
    cd "sqlcipher-${SQLCIPHER_VERSION}"; \
    ./configure --disable-shared --enable-static --enable-tempstore=yes \
        --with-crypto-lib=openssl CFLAGS="-DSQLITE_HAS_CODEC -O2"; \
    make -j"$(nproc)" libsqlcipher.la; \
    mkdir -p /out/lib /out/include; \
    cp .libs/libsqlcipher.a /out/lib/libsqlite3.a; \
    cp sqlite3.h /out/include/sqlite3.h

FROM scratch
LABEL org.opencontainers.image.source="https://github.com/letya999/h-hub"
COPY --from=build /out/lib/libsqlite3.a /lib/libsqlite3.a
COPY --from=build /out/include/sqlite3.h /include/sqlite3.h
