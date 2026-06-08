FROM golang:1.22-alpine AS builder
WORKDIR /build
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN go mod tidy && CGO_ENABLED=0 go build -o quota-sentinel .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /build/quota-sentinel .
COPY --from=builder /build/config.example.yaml ./config.yaml
EXPOSE 8080
VOLUME ["/app/data"]
CMD ["./quota-sentinel"]
