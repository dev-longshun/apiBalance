# 编译阶段跑在构建机原生架构上，交叉编译出目标架构二进制（Mac 上构建 amd64 镜像不走 QEMU 模拟）
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS builder
ARG TARGETOS TARGETARCH
WORKDIR /build
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o upstream-balance .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /build/upstream-balance .
COPY --from=builder /build/config.example.yaml ./config.yaml
EXPOSE 8080
VOLUME ["/app/data"]
CMD ["./upstream-balance"]
