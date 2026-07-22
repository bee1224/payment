FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN go build -trimpath -o /merchant-sandbox ./cmd/merchant-sandbox

FROM alpine:3.20
RUN adduser -D -u 10001 merchant
USER merchant
WORKDIR /app
COPY --from=build /merchant-sandbox /usr/local/bin/merchant-sandbox
EXPOSE 8281
ENTRYPOINT ["merchant-sandbox"]
