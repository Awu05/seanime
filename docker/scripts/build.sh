#!/usr/bin/env bash
set -e

TAG="${1:-latest}"
REGISTRY="ghcr.io/awu05/seanime"

echo "Building seanime Docker images with tag: ${TAG}"

echo "Building Default image..."
docker build -t ${REGISTRY}:${TAG} --target base -f Dockerfile .

echo "Building Rootless image..."
docker build -t ${REGISTRY}:${TAG}-rootless --target rootless -f Dockerfile .

echo "Building HwAccel image..."
docker build -t ${REGISTRY}:${TAG}-hwaccel --target hwaccel -f Dockerfile .

echo ""
echo "Build complete!"
echo "  ${REGISTRY}:${TAG}"
echo "  ${REGISTRY}:${TAG}-rootless"
echo "  ${REGISTRY}:${TAG}-hwaccel"
