LABEL org.opencontainers.image.source = "https://github.com/kralamoure/d1proxy"

FROM golang:1.15.2-alpine3.12 AS builder

RUN apk add git
RUN git config --global credential.helper store
COPY .git-credentials /root/.git-credentials

WORKDIR /app
COPY . .

RUN go install -v ./...

FROM alpine:3.12.0
WORKDIR /app
COPY --from=builder /go/bin/ .

ENTRYPOINT ["./d1proxy"]
