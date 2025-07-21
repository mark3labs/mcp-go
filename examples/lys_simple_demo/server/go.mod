module lys_simple_demo/server

go 1.23

toolchain go1.23.3

require github.com/mark3labs/mcp-go v0.34.0

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
)

replace github.com/mark3labs/mcp-go => ../../..
