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
FROM alpine:3.19

WORKDIR /app

# Install runtime dependencies including Python for DuoSubs
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    python3 \
    py3-pip \
    ffmpeg \
    opencc

# Install DuoSubs Python package
RUN pip3 install --no-cache-dir duosubs --break-system-packages

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

