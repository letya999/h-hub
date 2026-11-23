# hhub-dev: pinned toolchain for `just check` (rule 26).
# Build: `just dev-image`. Tools are pinned by version and sha256.
FROM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf

ARG GOLANGCI_VERSION=2.15.0
ARG GOLANGCI_SHA256=9b7fe6a6bf6334b98decf8d86b11415ea7889afdabdd1959fdce23af2a9ebcea
ARG GITLEAKS_VERSION=8.30.1
ARG GITLEAKS_SHA256=551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb
ARG JUST_VERSION=1.59.0
ARG JUST_SHA256=409044617b6f06517c1035e55ca38c8c856ff8b650e41bc2aa1ef82d5903a23e

RUN set -eu; \
    apk add --no-cache git docker-cli ca-certificates; \
    cd /tmp; \
    wget -q "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_VERSION}/golangci-lint-${GOLANGCI_VERSION}-linux-amd64.tar.gz"; \
    test "$(sha256sum "golangci-lint-${GOLANGCI_VERSION}-linux-amd64.tar.gz" | cut -d' ' -f1)" = "${GOLANGCI_SHA256}"; \
    tar -xzf "golangci-lint-${GOLANGCI_VERSION}-linux-amd64.tar.gz" --strip-components=1 -C /usr/local/bin "golangci-lint-${GOLANGCI_VERSION}-linux-amd64/golangci-lint"; \
    wget -q "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz"; \
    test "$(sha256sum "gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz" | cut -d' ' -f1)" = "${GITLEAKS_SHA256}"; \
    tar -xzf "gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz" -C /usr/local/bin gitleaks; \
    wget -q "https://github.com/casey/just/releases/download/${JUST_VERSION}/just-${JUST_VERSION}-x86_64-unknown-linux-musl.tar.gz"; \
    test "$(sha256sum "just-${JUST_VERSION}-x86_64-unknown-linux-musl.tar.gz" | cut -d' ' -f1)" = "${JUST_SHA256}"; \
    tar -xzf "just-${JUST_VERSION}-x86_64-unknown-linux-musl.tar.gz" -C /usr/local/bin just; \
    rm -f /tmp/*.tar.gz; \
    git config --global --add safe.directory /src

WORKDIR /src
