# h-hub service image: one binary per service (rule 7).
# docker build -f deploy/svc.Dockerfile --build-arg SVC=<name> -t hhub-<name>:local .
FROM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
ARG SVC
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -o /out/svc ./services/$SVC

FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
COPY --from=build /out/svc /svc
COPY deploy/hermes-image.ref /usr/share/hhub/hermes-image.ref
COPY deploy/descriptor.yaml /usr/share/hhub/descriptor.yaml
COPY services/harness-runtime/plugins /hhub-plugins
RUN touch /hhub-empty-env
EXPOSE 9000
ENTRYPOINT ["/svc"]
