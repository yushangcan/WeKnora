#!/usr/bin/env bash
set -euo pipefail
# /src is read-only. Keep compiled files and generated fixtures out of checkout.
git config --global --add safe.directory /src
mkdir -p /build/runtime/config /build/runtime/dataset/samples
go test ./cmd/evaluation ./internal/evaluation ./internal/models/usage \
  ./dataset/cmrc2018/convert -count=1 -timeout=180s
go build -buildvcs=true -ldflags='-X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn' -o /build/weknora ./cmd/server
CGO_ENABLED=0 go build -o /build/evaluation ./cmd/evaluation
go run ./ci/evaluation/fixture /build/runtime/dataset/samples
cp config/config.yaml /build/runtime/config/
cp -R config/prompt_templates /build/runtime/config/
cp -R migrations /build/runtime/
cp VERSION /build/runtime/
git rev-parse HEAD > /build/source-commit.txt
go version -m /build/weknora > /build/build-info.txt
