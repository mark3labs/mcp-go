# Structured Content Example

This example shows how to return `structuredContent` in tool result with corresponding `OutputSchema`.

Defined in the MCP spec here: https://modelcontextprotocol.io/specification/2025-06-18/server/tools#structured-content

## Usage

Define a struct for your input:

```go
type WeatherRequest struct {
    Location string `json:"location,required" jsonschema_description:"City or location"`
    Units    string `json:"units,omitempty" jsonschema_description:"celsius or fahrenheit" jsonschema:"enum=celsius,enum=fahrenheit"`
}
```

Define a struct for your output:

```go
type WeatherResponse struct {
    Location    string  `json:"location" jsonschema_description:"The location"`
    Temperature float64 `json:"temperature" jsonschema_description:"Current temperature"`
    Conditions  string  `json:"conditions" jsonschema_description:"Weather conditions"`
}
```

Add it to your tool:

```go
tool := mcp.NewTool("get_weather",
    mcp.WithDescription("Get weather information"),
    mcp.WithInputSchema[WeatherRequest](),
    mcp.WithOutputSchema[WeatherResponse](),
)
```

Return structured data in tool result:

```go
func weatherHandler(ctx context.Context, request mcp.CallToolRequest, args WeatherRequest) (*mcp.CallToolResult, error) {
    response := WeatherResponse{
        Location:    args.Location,
        Temperature: 25.0,
        Conditions:  "Cloudy",
    }
    
    fallbackText := fmt.Sprintf("Weather in %s: %.1f°C, %s", 
        response.Location, response.Temperature, response.Conditions)
    
    return mcp.NewToolResultStructured(response, fallbackText), nil
}
```

See [main.go](./main.go) for more examples.