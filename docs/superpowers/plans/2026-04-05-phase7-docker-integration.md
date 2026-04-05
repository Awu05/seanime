# Phase 7: Docker Integration — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the Dockerfile, docker-compose examples, and entrypoint from the separate seanime-docker repo into the main seanime repo. Add multi-user env var support. Maintain all 4 image variants (default, rootless, hwaccel, cuda).

**Architecture:** The existing seanime-docker repo clones seanime from GitHub during build. The new in-repo Dockerfile uses `COPY` from the build context instead. The entrypoint script is extended to pass `SEANIME_ADMIN_USERNAME`, `SEANIME_ADMIN_PASSWORD`, and `SEANIME_INSTANCE_ACCESS_CODE` env vars to the Go binary. Supervisord manages both qBittorrent and seanime processes.

**Tech Stack:** Docker (multi-stage builds), Alpine Linux, supervisord, qbittorrent-nox, FFmpeg

---

## File Structure

### New Files (all in main seanime repo)

| File | Responsibility |
|------|---------------|
| `Dockerfile` | Multi-stage build: node → go → 4 runtime variants (base, rootless, hwaccel) |
| `Dockerfile.cuda` | NVIDIA CUDA variant (Ubuntu-based) |
| `.dockerignore` | Exclude unnecessary files from build context |
| `docker/config/entrypoint.sh` | Supervisord orchestration for qBittorrent + seanime + env var bootstrap |
| `docker/examples/01-default/docker-compose.yml` | Default (root) compose config |
| `docker/examples/02-rootless/docker-compose.yml` | Rootless compose config |
| `docker/examples/03-hwaccel/docker-compose.yml` | Intel HW acceleration compose config |
| `docker/examples/04-hwaccel-cuda/docker-compose.yml` | NVIDIA CUDA compose config |
| `docker/scripts/build.sh` | Local build script for all 4 variants |
| `docker/scripts/get-cuda-version.sh` | Fetches latest CUDA base image tag |

---

## Tasks

### Task 1: Create .dockerignore

**Files:**
- Create: `.dockerignore`

- [ ] **Step 1: Create .dockerignore**

Create `.dockerignore` at repo root:

```
.git
.github
.gitignore
.vscode
.idea

# Documentation
docs

# Electron app (not needed for Docker)
seanime-denshi

# Code generation
codegen

# Build artifacts
node_modules
dist
build
bin

# Test files
*_test.go

# Local development
.env*
*.db
local_testing
dist-server
```

- [ ] **Step 2: Commit**

```bash
git add .dockerignore
git commit -m "feat: add .dockerignore for Docker builds"
```

---

### Task 2: Create Entrypoint Script

**Files:**
- Create: `docker/config/entrypoint.sh`

- [ ] **Step 1: Create entrypoint script**

Create `docker/config/entrypoint.sh`:

```bash
#!/bin/sh
set -e

QBIT_WEBUI_PORT="${QBIT_WEBUI_PORT:-8081}"
QBIT_USERNAME="${QBIT_USERNAME:-admin}"
QBIT_PASSWORD="${QBIT_PASSWORD:-adminadmin}"

# Determine qBittorrent config directory based on user
if [ "$(id -u)" = "0" ]; then
    QBIT_CONF_DIR="/root/.config/qBittorrent"
else
    QBIT_CONF_DIR="$(eval echo ~$(whoami))/.config/qBittorrent"
fi

mkdir -p "$QBIT_CONF_DIR"

# Generate PBKDF2-HMAC-SHA512 password hash (100,000 iterations) using Python
generate_qbit_password() {
    python3 -c "
import hashlib, os, base64, sys
password = sys.argv[1].encode()
salt = os.urandom(16)
dk = hashlib.pbkdf2_hmac('sha512', password, salt, 100000)
print('@ByteArray(' + base64.b64encode(salt).decode() + ':' + base64.b64encode(dk).decode() + ')')
" "$1"
}

PASSWORD_HASH=$(generate_qbit_password "$QBIT_PASSWORD")
QBIT_CONF="$QBIT_CONF_DIR/qBittorrent.conf"

# Write qBittorrent config on first run only
if [ ! -f "$QBIT_CONF" ]; then
    cat > "$QBIT_CONF" <<EOF
[Preferences]
WebUI\Port=${QBIT_WEBUI_PORT}
WebUI\Username=${QBIT_USERNAME}
WebUI\Password_PBKDF2="${PASSWORD_HASH}"
WebUI\CSRFProtection=false
WebUI\ClickjackingProtection=false
WebUI\HostHeaderValidation=false
WebUI\LocalHostAuth=false
WebUI\MaxAuthenticationFailCount=0
WebUI\BanDuration=0

[BitTorrent]
Session\DefaultSavePath=/downloads

[Meta]
MigrationVersion=6
EOF
else
    # Helper to update or add a setting under [Preferences]
    update_setting() {
        KEY="$1"
        VALUE="$2"
        ESCAPED_KEY=$(echo "$KEY" | sed 's|\\|\\\\|g')
        if grep -q "^${ESCAPED_KEY}=" "$QBIT_CONF"; then
            sed -i "s|^${ESCAPED_KEY}=.*|${KEY}=${VALUE}|" "$QBIT_CONF"
        else
            sed -i "/^\[Preferences\]/a ${KEY}=${VALUE}" "$QBIT_CONF"
        fi
    }

    # Update credentials and port from env vars
    update_setting "WebUI\\\\Port" "${QBIT_WEBUI_PORT}"
    update_setting "WebUI\\\\Username" "${QBIT_USERNAME}"
    update_setting "WebUI\\\\Password_PBKDF2" "\"${PASSWORD_HASH}\""

    # Ensure security settings are present
    update_setting "WebUI\\\\CSRFProtection" "false"
    update_setting "WebUI\\\\ClickjackingProtection" "false"
    update_setting "WebUI\\\\HostHeaderValidation" "false"
    update_setting "WebUI\\\\LocalHostAuth" "false"
    update_setting "WebUI\\\\MaxAuthenticationFailCount" "0"
    update_setting "WebUI\\\\BanDuration" "0"
fi

# Generate supervisord config with the configured port
cat > /tmp/supervisord.conf <<EOF
[supervisord]
nodaemon=true
logfile=/var/log/supervisor/supervisord.log
pidfile=/tmp/supervisord.pid

[program:qbittorrent]
command=qbittorrent-nox --webui-port=${QBIT_WEBUI_PORT}
autostart=true
autorestart=true
priority=1
stdout_logfile=/dev/stdout
stdout_logfile_maxbytes=0
stderr_logfile=/dev/stderr
stderr_logfile_maxbytes=0

[program:seanime]
command=/app/seanime --host 0.0.0.0
directory=/app
autostart=true
autorestart=true
priority=10
startsecs=5
stdout_logfile=/dev/stdout
stdout_logfile_maxbytes=0
stderr_logfile=/dev/stderr
stderr_logfile_maxbytes=0
EOF

exec /usr/bin/supervisord -c /tmp/supervisord.conf
```

Note: The `SEANIME_ADMIN_USERNAME`, `SEANIME_ADMIN_PASSWORD`, and `SEANIME_INSTANCE_ACCESS_CODE` env vars are read directly by the Go binary at startup (implemented in Phase 1's `bootstrapAdminFromEnv`), so the entrypoint doesn't need to handle them — they just need to be passed through to the container via docker-compose.

- [ ] **Step 2: Make executable**

```bash
chmod +x docker/config/entrypoint.sh
```

- [ ] **Step 3: Commit**

```bash
git add docker/config/entrypoint.sh
git commit -m "feat: add Docker entrypoint script with qBittorrent + supervisord"
```

---

### Task 3: Create Main Dockerfile

**Files:**
- Create: `Dockerfile`

- [ ] **Step 1: Create multi-stage Dockerfile**

Create `Dockerfile` at repo root:

```dockerfile
# syntax=docker/dockerfile:1.4

# Stage 1: Node.js Builder
FROM --platform=$BUILDPLATFORM node:latest AS node-builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /tmp/build

# Copy only package files first for better caching
COPY seanime-web/package*.json ./

# Install dependencies with cache mount
RUN --mount=type=cache,target=/root/.npm \
    npm install

# Copy source code after dependencies are installed
COPY seanime-web ./

RUN npm run build

# Stage 2: Go Builder
FROM --platform=$BUILDPLATFORM golang:latest AS go-builder

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

WORKDIR /tmp/build

# Copy only go.mod and go.sum first for better caching
COPY go.mod go.sum ./

# Download Go modules with cache
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source code after dependencies are downloaded
COPY . ./
COPY --from=node-builder /tmp/build/out /tmp/build/web

# Handle armv7 (32-bit ARM) builds specifically
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    if [ "$TARGETARCH" = "arm" ] && [ "$TARGETVARIANT" = "v7" ]; then \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=7 go build -o seanime -trimpath -ldflags="-s -w"; \
    else \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o seanime -trimpath -ldflags="-s -w"; \
    fi

# Stage 3: Common Base
FROM --platform=$TARGETPLATFORM alpine:latest AS common-base

# Install common dependencies
RUN apk add --no-cache ca-certificates tzdata curl qbittorrent-nox supervisor python3

# Create directories for supervisord
RUN mkdir -p /var/log/supervisor

# Stage 4: Default (Root) Variant
FROM common-base AS base

# Install standard ffmpeg
RUN apk add --no-cache ffmpeg

# Copy binary and entrypoint
COPY --from=go-builder /tmp/build/seanime /app/
COPY docker/config/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

WORKDIR /app
EXPOSE 43211 8081

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:43211 || exit 1

CMD ["/app/entrypoint.sh"]

# Stage 5: Rootless Variant
FROM common-base AS rootless

# Create user
RUN addgroup -S seanime -g 1000 && \
    adduser -S seanime -G seanime -u 1000 -s /sbin/nologin

# Install standard ffmpeg
RUN apk add --no-cache ffmpeg

# Copy binary and entrypoint with ownership
COPY --from=go-builder --chown=1000:1000 /tmp/build/seanime /app/
COPY --chown=1000:1000 docker/config/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# Ensure directories are writable by seanime user
RUN chown -R 1000:1000 /var/log/supervisor

USER 1000
WORKDIR /app
EXPOSE 43211 8081

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:43211 || exit 1

CMD ["/app/entrypoint.sh"]

# Stage 6: Hardware Acceleration Variant
FROM --platform=$TARGETPLATFORM alpine:edge AS hwaccel

# Install common dependencies
RUN apk add --no-cache ca-certificates tzdata curl qbittorrent-nox supervisor python3

# Create directories for supervisord
RUN mkdir -p /var/log/supervisor

ARG TARGETARCH

# Create user and add to group
RUN addgroup -S seanime -g 1000 && \
    adduser -S seanime -G seanime -u 1000

# Install Jellyfin FFmpeg and Intel drivers (amd64 only)
RUN apk update && \
    PACKAGES="jellyfin-ffmpeg mesa-va-gallium opencl-icd-loader" && \
    if [ "$TARGETARCH" = "amd64" ]; then \
    PACKAGES="$PACKAGES libva-intel-driver intel-media-driver libvpl"; \
    apk add --no-cache --repository=https://dl-cdn.alpinelinux.org/alpine/edge/testing onevpl-intel-gpu; \
    fi && \
    apk add --no-cache --repository=https://repo.jellyfin.org/releases/alpine/ $PACKAGES && \
    chmod +x /usr/lib/jellyfin-ffmpeg/ffmpeg /usr/lib/jellyfin-ffmpeg/ffprobe && \
    ln -s /usr/lib/jellyfin-ffmpeg/ffmpeg /usr/bin/ffmpeg && \
    ln -s /usr/lib/jellyfin-ffmpeg/ffprobe /usr/bin/ffprobe

# Copy binary and entrypoint with ownership
COPY --from=go-builder --chown=1000:1000 /tmp/build/seanime /app/
COPY --chown=1000:1000 docker/config/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# Ensure directories are writable by seanime user
RUN chown -R 1000:1000 /var/log/supervisor

USER 1000
WORKDIR /app
EXPOSE 43211 8081

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:43211 || exit 1

CMD ["/app/entrypoint.sh"]
```

- [ ] **Step 2: Commit**

```bash
git add Dockerfile
git commit -m "feat: add multi-stage Dockerfile with 4 variants"
```

---

### Task 4: Create CUDA Dockerfile

**Files:**
- Create: `Dockerfile.cuda`

- [ ] **Step 1: Create CUDA Dockerfile**

Create `Dockerfile.cuda` at repo root. This uses the NVIDIA CUDA Ubuntu base. Read the existing `Dockerfile.cuda` from `c:\Users\awu05\OneDrive\Documents\Github\seanime-docker\Dockerfile.cuda`, copy it, and change:
- Remove the `source` stage (git clone)
- Replace `COPY --from=source` with `COPY`
- Change `COPY config/entrypoint.sh` to `COPY docker/config/entrypoint.sh`

- [ ] **Step 2: Commit**

```bash
git add Dockerfile.cuda
git commit -m "feat: add CUDA Dockerfile variant"
```

---

### Task 5: Create Docker Compose Examples

**Files:**
- Create: `docker/examples/01-default/docker-compose.yml`
- Create: `docker/examples/02-rootless/docker-compose.yml`
- Create: `docker/examples/03-hwaccel/docker-compose.yml`
- Create: `docker/examples/04-hwaccel-cuda/docker-compose.yml`

- [ ] **Step 1: Create default compose**

Create `docker/examples/01-default/docker-compose.yml`:

```yaml
services:
  seanime:
    build:
      context: ../..
      dockerfile: Dockerfile
      target: base
    container_name: seanime
    environment:
      - QBIT_WEBUI_PORT=8081
      - QBIT_USERNAME=admin
      - QBIT_PASSWORD=adminadmin
      # Multi-user auth (optional — uncomment to auto-create admin on first start)
      # - SEANIME_ADMIN_USERNAME=admin
      # - SEANIME_ADMIN_PASSWORD=changeme
      # - SEANIME_INSTANCE_ACCESS_CODE=family123
    ports:
      - "3211:43211"
      - "8081:8081"
    volumes:
      - ./config:/root/.config
      - ./anime:/anime
      - ./downloads:/downloads
    restart: unless-stopped
```

- [ ] **Step 2: Create rootless compose**

Create `docker/examples/02-rootless/docker-compose.yml`:

```yaml
services:
  seanime:
    build:
      context: ../..
      dockerfile: Dockerfile
      target: rootless
    container_name: seanime
    environment:
      - QBIT_WEBUI_PORT=8081
      - QBIT_USERNAME=admin
      - QBIT_PASSWORD=adminadmin
      # Multi-user auth (optional)
      # - SEANIME_ADMIN_USERNAME=admin
      # - SEANIME_ADMIN_PASSWORD=changeme
      # - SEANIME_INSTANCE_ACCESS_CODE=family123
    ports:
      - "3211:43211"
      - "8081:8081"
    volumes:
      - ./config:/home/seanime/.config
      - ./anime:/anime
      - ./downloads:/downloads
    restart: unless-stopped
```

- [ ] **Step 3: Create hwaccel compose**

Create `docker/examples/03-hwaccel/docker-compose.yml`:

```yaml
services:
  seanime:
    build:
      context: ../..
      dockerfile: Dockerfile
      target: hwaccel
    container_name: seanime
    environment:
      - QBIT_WEBUI_PORT=8081
      - QBIT_USERNAME=admin
      - QBIT_PASSWORD=adminadmin
      # Multi-user auth (optional)
      # - SEANIME_ADMIN_USERNAME=admin
      # - SEANIME_ADMIN_PASSWORD=changeme
      # - SEANIME_INSTANCE_ACCESS_CODE=family123
    ports:
      - "3211:43211"
      - "8081:8081"
    volumes:
      - ./config:/home/seanime/.config
      - ./anime:/anime
      - ./downloads:/downloads
    devices:
      - /dev/dri:/dev/dri
    group_add:
      - video
      - render
    restart: unless-stopped
```

- [ ] **Step 4: Create CUDA compose**

Create `docker/examples/04-hwaccel-cuda/docker-compose.yml`:

```yaml
services:
  seanime:
    build:
      context: ../..
      dockerfile: Dockerfile.cuda
    container_name: seanime
    runtime: nvidia
    environment:
      - QBIT_WEBUI_PORT=8081
      - QBIT_USERNAME=admin
      - QBIT_PASSWORD=adminadmin
      - NVIDIA_VISIBLE_DEVICES=all
      - NVIDIA_DRIVER_CAPABILITIES=all
      # Multi-user auth (optional)
      # - SEANIME_ADMIN_USERNAME=admin
      # - SEANIME_ADMIN_PASSWORD=changeme
      # - SEANIME_INSTANCE_ACCESS_CODE=family123
    ports:
      - "3211:43211"
      - "8081:8081"
    volumes:
      - ./config:/home/seanime/.config
      - ./anime:/anime
      - ./downloads:/downloads
    group_add:
      - video
    restart: unless-stopped
```

- [ ] **Step 5: Commit**

```bash
git add docker/examples/
git commit -m "feat: add docker-compose examples for all 4 variants"
```

---

### Task 6: Create Build Script

**Files:**
- Create: `docker/scripts/build.sh`
- Create: `docker/scripts/get-cuda-version.sh`

- [ ] **Step 1: Create build script**

Create `docker/scripts/build.sh`:

```bash
#!/usr/bin/env bash
set -e

TAG="${1:-latest}"
REGISTRY="ghcr.io/awu05/seanime"

echo "Building seanime Docker images with tag: ${TAG}"

echo "Building Default image..."
docker build -t ${REGISTRY}:${TAG} --target base .

echo "Building Rootless image..."
docker build -t ${REGISTRY}:${TAG}-rootless --target rootless .

echo "Building HwAccel image..."
docker build -t ${REGISTRY}:${TAG}-hwaccel --target hwaccel .

echo ""
echo "Build complete!"
echo "  ${REGISTRY}:${TAG}"
echo "  ${REGISTRY}:${TAG}-rootless"
echo "  ${REGISTRY}:${TAG}-hwaccel"
```

- [ ] **Step 2: Create CUDA version helper**

Copy `c:\Users\awu05\OneDrive\Documents\Github\seanime-docker\scripts\get-cuda-version.sh` to `docker/scripts/get-cuda-version.sh`.

- [ ] **Step 3: Make scripts executable**

```bash
chmod +x docker/scripts/build.sh docker/scripts/get-cuda-version.sh
```

- [ ] **Step 4: Commit**

```bash
git add docker/scripts/
git commit -m "feat: add Docker build scripts"
```

---

### Task 7: Verify Docker Build

- [ ] **Step 1: Test Docker build (rootless variant)**

```bash
docker build -t seanime-test --target rootless .
```

Expected: Successful build

- [ ] **Step 2: Verify container starts**

```bash
docker run --rm -p 3211:43211 -e SEANIME_ADMIN_USERNAME=admin -e SEANIME_ADMIN_PASSWORD=test123 seanime-test
```

Verify: seanime starts on port 43211, health check passes

- [ ] **Step 3: Clean up**

```bash
docker rmi seanime-test
```

- [ ] **Step 4: Commit if any fixes needed**

```bash
git add -A
git commit -m "fix: Docker build adjustments"
```
