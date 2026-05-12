FROM golang:1.26-alpine

ENV TZ=Asia/Shanghai
ENV GOPROXY=https://goproxy.cn,direct
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories && apk add --no-cache tzdata

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN go get github.com/larksuite/oapi-sdk-go/v3/ws@latest && go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/claw/

# 默认启动命令（需要通过 --env-file 或 -e 注入环境变量）
CMD ["./main"]
