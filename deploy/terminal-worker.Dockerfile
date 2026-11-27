# The terminal profile carries only cellinit and the command-line tools.
FROM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -o /out/cellinit ./services/spawn-controller/cmd/cellinit

FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
RUN apk add --no-cache nodejs python3 uv git ripgrep && mkdir -p /workspace /run/hhub
COPY --from=build /out/cellinit /cellinit
USER 65534:65534
WORKDIR /workspace
ENTRYPOINT ["/cellinit"]
