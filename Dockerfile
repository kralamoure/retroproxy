FROM golang:1.16.4-buster AS builder

WORKDIR /app
COPY . .

RUN go install -v ./...

FROM ubuntu:20.04

LABEL org.opencontainers.image.source="https://github.com/kralamoure/retroproxy"

RUN apt update && apt upgrade -y

WORKDIR /app
COPY --from=builder /go/bin/ .

ENTRYPOINT ["./retroproxy"]
