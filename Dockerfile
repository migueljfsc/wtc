FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /wtc ./cmd/wtc

FROM scratch
# CA bundle: the poller talks HTTPS to api.github.com.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /wtc /wtc
VOLUME /data
EXPOSE 8484
ENTRYPOINT ["/wtc"]
CMD ["serve", "--config", "/etc/wtc/wtc.yaml"]
