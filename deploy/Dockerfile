FROM golang:alpine AS builder
LABEL stage=gobuilder
ENV CGO_ENABLED 0
ENV GOPROXY https://goproxy.cn,direct
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
RUN apk update --no-cache && apk add --no-cache tzdata
WORKDIR /build
ADD go.mod .
ADD go.sum .
RUN go mod download
COPY . .
RUN go build -ldflags="-s -w" -o /app/fusionn cmd/api/main.go

FROM alpine
COPY --from=builder /usr/share/zoneinfo/Asia/Shanghai /usr/share/zoneinfo/Asia/Shanghai
ENV TZ Asia/Shanghai
WORKDIR /app
COPY --from=builder /app/fusionn /app/fusionn
COPY --from=builder /build/init.sh /app/init.sh
RUN chmod +x /app/init.sh
COPY --from=builder /build/config.yml /app/config.yml
RUN apk update --no-cache && apk add --no-cache ffmpeg
RUN mkdir tmp

CMD ["./init.sh"]
