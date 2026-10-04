#!/bin/sh
set -eu
go test -race ./...
go vet ./...
test -z "$(gofmt -l cmd internal)"
go build ./cmd/paas
python3 -m py_compile tools/*.py deploy/*.py
bash -n deploy/bootstrap.sh
sh -n deploy/renew-certificate.sh
