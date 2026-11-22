# hhub-hubctl image: hubctl + keydist on PATH (keydist container).
FROM golang:1.27-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/hubctl ./cmd/hubctl \
    && CGO_ENABLED=0 go build -trimpath -o /out/keydist ./cmd/keydist

FROM alpine:3.21@sha256:bc21487101ae47172509130ee1092cc273a831968d64b617776d1f69c81e42ab
COPY --from=build /out/hubctl /out/keydist /usr/local/bin/
ENTRYPOINT ["hubctl"]
