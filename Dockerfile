FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/http-sink .

FROM alpine:3.22
LABEL org.opencontainers.image.authors="jecklgamis@gmail.com"

RUN apk add --no-cache curl && addgroup -S app && adduser -S app -G app

WORKDIR /app
COPY --from=build /out/http-sink .
COPY --chown=app:app server.crt server.key ./

USER app
EXPOSE 38080
EXPOSE 8443
ENTRYPOINT ["/app/http-sink"]
