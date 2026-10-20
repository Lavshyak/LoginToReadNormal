# syntax=docker/dockerfile:1

# --- build stage ----------------------------------------------------------
FROM golang:1.23-alpine AS build
WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/app .

# --- runtime stage --------------------------------------------------------
FROM alpine:3.20
RUN adduser -D -H -u 10001 app \
 && mkdir -p /data \
 && chown -R app:app /data

COPY --from=build /out/app /usr/local/bin/app

USER app
ENV DB_PATH=/data/app.db \
    LISTEN_ADDR=:8080
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/app"]
