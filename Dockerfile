FROM golang:1.20 AS build

WORKDIR /go/src/app
COPY . .

RUN go mod download
RUN CGO_ENABLED=0 go build -o /go/bin/retroproxy ./cmd/retroproxy

FROM gcr.io/distroless/static-debian11:latest

LABEL org.opencontainers.image.source="https://github.com/kralamoure/retroproxy"

COPY --from=build /go/bin/retroproxy /
ENTRYPOINT ["/retroproxy"]
