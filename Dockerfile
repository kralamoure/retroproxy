FROM golang:1.15.2-buster AS builder

RUN git config --global credential.helper store
COPY .git-credentials /root/.git-credentials

WORKDIR /app
COPY . .

RUN go install -v ./...

FROM ubuntu:20.04

LABEL org.opencontainers.image.source="https://github.com/kralamoure/d1proxy"

RUN apt-get update && apt-get install -y

WORKDIR /app
COPY --from=builder /go/bin/ .

ENTRYPOINT ["./d1proxy"]
