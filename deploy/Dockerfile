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

FROM debian:bookworm-slim
COPY --from=builder /usr/share/zoneinfo/Asia/Shanghai /usr/share/zoneinfo/Asia/Shanghai
ENV TZ Asia/Shanghai
WORKDIR /app

# Install required packages
RUN apt-get update && apt-get install -y \
  wget \
  tar \
  curl \
  jq \
  ffmpeg \
  && rm -rf /var/lib/apt/lists/*

# Download appropriate assfonts binary based on architecture
RUN LATEST_VERSION=$(curl -s https://api.github.com/repos/wyzdwdz/assfonts/releases/latest | jq -r .tag_name | sed 's/^v//') && \
  case "$(dpkg --print-architecture)" in \
  "amd64") ARCH="x86_64" ;; \
  "arm64") ARCH="aarch64" ;; \
  *) echo "Unsupported architecture: $(dpkg --print-architecture)" && exit 1 ;; \
  esac && \
  echo "Downloading assfonts for architecture: $ARCH" && \
  wget -q https://github.com/wyzdwdz/assfonts/releases/download/v${LATEST_VERSION}/assfonts-v${LATEST_VERSION}-${ARCH}-Linux.tar.gz && \
  tar xzf assfonts-v${LATEST_VERSION}-${ARCH}-Linux.tar.gz && \
  mv bin/assfonts /usr/local/bin/ && \
  chmod +x /usr/local/bin/assfonts && \
  rm -rf bin share assfonts-gui.AppImage assfonts-v${LATEST_VERSION}-${ARCH}-Linux.tar.gz

COPY --from=builder /app/fusionn /app/fusionn
COPY --from=builder /build/init.sh /app/init.sh
ENV DOCKER_ENV=true
RUN chmod +x /app/init.sh
COPY --from=builder /build/config.yml /app/config.yml.default
RUN mkdir tmp
COPY asset/fonts /app/asset/fonts

CMD ["./init.sh"]
