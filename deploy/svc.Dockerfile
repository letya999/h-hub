# h-hub service image: one binary per service (rule 7).
# docker build -f deploy/svc.Dockerfile --build-arg SVC=<name> -t hhub-<name>:local .
FROM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build
ARG SVC
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -o /out/svc ./services/$SVC

FROM alpine:3.21@sha256:bc21487101ae47172509130ee1092cc273a831968d64b617776d1f69c81e42ab
COPY --from=build /out/svc /svc
COPY deploy/hermes-image.ref /usr/share/hhub/hermes-image.ref
COPY services/harness-runtime/plugins /hhub-plugins
EXPOSE 9000
ENTRYPOINT ["/svc"]
