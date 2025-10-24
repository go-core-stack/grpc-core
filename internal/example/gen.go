package example

//go:generate protoc -I . -I ../../ -I ../third_party --go_out=. --go_opt=paths=source_relative --sdk_out . --sdk_opt paths=source_relative --routes_out . --routes_opt paths=source_relative test.proto
