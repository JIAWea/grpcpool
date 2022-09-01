#!/bin/bash
trap "rm grpc_server;kill 0" EXIT

cd ../example || exit
go build -o grpc_server
./grpc_server -port=50051 &
./grpc_server -port=50052 &
./grpc_server -port=50053
