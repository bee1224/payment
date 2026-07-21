FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/payment-api ./cmd/api

FROM alpine:3.20

RUN apk add --no-cache tzdata

WORKDIR /app
COPY --from=builder /out/payment-api /app/payment-api
COPY config/config.example.yaml /app/config/config.yaml
COPY migrations /app/migrations

ENV APP_ENV=production
ENV APP_PORT=8080

EXPOSE 8080
CMD ["/app/payment-api"]
