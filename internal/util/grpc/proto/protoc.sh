#!/bin/bash
# Regenerate the tnet tunnel gRPC code.
#
# Tool versions matter: the checked-in files were generated with
#   protoc                36.x
#   protoc-gen-go         1.36.x
#   protoc-gen-go-grpc    1.6.x
# Older protoc-gen-go-grpc (<1.5) emits a non-generic stream type, which
# tnet_grpcx.go no longer references - regenerate, don't hand-edit the .pb.go.
set -euo pipefail
cd "$(dirname "$0")"
protoc --go_out=. --go_opt=paths=source_relative \
	--go-grpc_out=. --go-grpc_opt=paths=source_relative \
	tnet.proto
