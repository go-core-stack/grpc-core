package api

//go:generate protoc -I . -I ../../internal/third_party --go_out=. --go_opt=paths=source_relative role.proto
