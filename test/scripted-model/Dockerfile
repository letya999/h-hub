# hhub-scripted-model: phase-3 test container with the built-in
# "answer with text" scenario. Test infrastructure, not a service.
# Build context is the repository root (just images).
FROM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY test/scripted-model ./test/scripted-model
RUN CGO_ENABLED=0 go build -trimpath -o /out/scripted-model ./test/scripted-model

FROM alpine:3.21@sha256:bc21487101ae47172509130ee1092cc273a831968d64b617776d1f69c81e42ab
COPY --from=build /out/scripted-model /scripted-model
EXPOSE 9000
ENTRYPOINT ["/scripted-model"]
