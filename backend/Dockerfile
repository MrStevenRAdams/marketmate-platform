FROM golang:1.25-bookworm@sha256:7fb09d8804035fbde8a84ed59ca9f46dd68c6f160f9d193e98d795d8d9e002ec AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

FROM debian:bookworm-slim@sha256:f06537653ac770703bc45b4b113475bd402f451e85223f0f2837acbf89ab020a
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/server .

EXPOSE 8080
CMD ["./server"]
