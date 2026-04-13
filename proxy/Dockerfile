FROM golang:1.26-alpine3.23@sha256:c2a1f7b2095d046ae14b286b18413a05bb82c9bca9b25fe7ff5efef0f0826166 AS builder
WORKDIR /app
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o proxy .

FROM alpine:3.22@sha256:8a1f59f88cfe431f5e2bba620cf0cb7607fae0b4e67e07a18bd58f5bbe6cad9c
RUN apk --no-cache add ca-certificates
RUN adduser -D -g "" appuser
WORKDIR /app
COPY --from=builder /app/proxy ./proxy
EXPOSE 8080
USER appuser
ENTRYPOINT ["./proxy"]
