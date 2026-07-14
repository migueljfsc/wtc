FROM golang:1.26-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION}" -o /app .

FROM scratch
COPY --from=build /app /app
EXPOSE 8080
ENTRYPOINT ["/app"]
