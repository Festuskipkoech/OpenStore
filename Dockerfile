FROM golang:1.24-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o openstore \
    ./cmd/openstore

FROM alpine:3.24

RUN apk add --no-cache ca-certificates tzdata

RUN addgroup -S openstore && adduser -S openstore -G openstore

WORKDIR /app

COPY --from=builder /build/openstore .

RUN chown openstore:openstore /app/openstore

USER openstore

EXPOSE 8080

ENTRYPOINT ["./openstore"]
