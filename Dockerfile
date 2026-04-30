FROM golang:1.26-alpine

ENV TZ=Asia/Shanghai
RUN apk add --no-cache tzdata

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/claw/

# 默认启动命令（需要通过 --env-file 或 -e 注入环境变量）
CMD ["./main"]
