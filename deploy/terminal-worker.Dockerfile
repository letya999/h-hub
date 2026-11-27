# The terminal profile carries only cellinit and the command-line tools.
FROM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -o /out/cellinit ./services/spawn-controller/cmd/cellinit

FROM alpine:3.21@sha256:bc21487101ae47172509130ee1092cc273a831968d64b617776d1f69c81e42ab
RUN apk add --no-cache nodejs python3 uv git ripgrep && mkdir -p /workspace /run/hhub
COPY --from=build /out/cellinit /cellinit
USER 65534:65534
WORKDIR /workspace
ENTRYPOINT ["/cellinit"]
