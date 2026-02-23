# ════════════════════════════════════════════════════════════════════════════
# STAGE 1: Build Go binary
# ════════════════════════════════════════════════════════════════════════════
FROM golang:1.23-alpine AS go-builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X github.com/fusionn/internal/version.Version=${VERSION}" \
    -o fusionn ./cmd/fusionn

# ════════════════════════════════════════════════════════════════════════════
# STAGE 2: Final image
# ════════════════════════════════════════════════════════════════════════════
FROM debian:bookworm-slim

WORKDIR /app

# Install runtime dependencies including Python for DuoSubs
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    python3 \
    python3-pip \
    ffmpeg \
    opencc \
    wget \
    && rm -rf /var/lib/apt/lists/*

# Install DuoSubs Python package
# Note: First run will download ~2GB LaBSE model to HuggingFace cache
RUN pip3 install --no-cache-dir duosubs --break-system-packages

# Download fusionn-font binary from GitHub releases
ARG FUSIONN_FONT_VERSION=v1.0.3
ARG TARGETARCH

RUN case ${TARGETARCH} in \
      "amd64") FUSIONN_FONT_ARCH="linux-amd64" ;; \
      "arm64") FUSIONN_FONT_ARCH="linux-arm64" ;; \
      *) echo "Unsupported architecture: ${TARGETARCH}" && exit 1 ;; \
    esac && \
    wget -O /usr/local/bin/fusionn-font \
      "https://github.com/weizsw/fusionn-font/releases/download/${FUSIONN_FONT_VERSION}/fusionn-font-${FUSIONN_FONT_ARCH}" && \
    chmod +x /usr/local/bin/fusionn-font && \
    fusionn-font --version

# Copy Go binary
COPY --from=go-builder /app/fusionn .

# Create data directories
RUN mkdir -p /data

# Set HuggingFace cache directory for model persistence
ENV HF_HOME=/root/.cache/huggingface
ENV ENV=production
ENV CONFIG_PATH=/app/config/config.yaml

EXPOSE 8080

CMD ["./fusionn"]

