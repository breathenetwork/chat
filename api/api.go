//go:generate protoc --go_out=plugins=grpc,paths=source_relative:. -I .:../ api.proto
package api

import "errors"

var NilRequestError = errors.New("nil request")
