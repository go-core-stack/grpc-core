package example

//go:generate protoc -I . -I ../../ -I ../third_party --routes_out . --routes_opt paths=source_relative test.proto
