# Readme for Codegen

This internal module contains code generation for producing a few repetitive
constructs, namely:

- The switch statement that handles the request dispatch
- The hook function types and the methods on the Hook struct

To invoke the code generation:

```
go generate ./...
```

## Development

- `request_handler.go.tmpl` generates `server/request_handler.go`, and
- `hooks.go.tmpl` generates `server/hooks.go`

Inside of `data.go` there is a struct with the inputs to both templates.

Note that the driver in `main.go` generates code and also pipes it through
`goimports` for formatting and imports cleanup.

