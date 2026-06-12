# ---- build ----
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/exotel-call-service ./cmd/server

# ---- run ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
USER app
COPY --from=build /out/exotel-call-service /usr/local/bin/exotel-call-service
EXPOSE 8080
ENTRYPOINT ["exotel-call-service"]
