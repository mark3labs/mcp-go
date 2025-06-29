package main

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Note: The jsonschema_description tag is added to the JSON schema as description
// Ideally use better descriptions, this is just an example
type WeatherRequest struct {
	Location string `json:"location" jsonschema_description:"City or location"`
	Units    string `json:"units,omitempty" jsonschema_description:"celsius or fahrenheit"`
}

type WeatherResponse struct {
	Location    string    `json:"location" jsonschema_description:"Location"`
	Temperature float64   `json:"temperature" jsonschema_description:"Temperature"`
	Units       string    `json:"units" jsonschema_description:"Units"`
	Conditions  string    `json:"conditions" jsonschema_description:"Weather conditions"`
	Timestamp   time.Time `json:"timestamp" jsonschema_description:"When retrieved"`
}

type UserProfile struct {
	ID    string   `json:"id" jsonschema_description:"User ID"`
	Name  string   `json:"name" jsonschema_description:"Full name"`
	Email string   `json:"email" jsonschema_description:"Email"`
	Tags  []string `json:"tags" jsonschema_description:"User tags"`
}

type UserRequest struct {
	UserID string `json:"userId" jsonschema_description:"User ID"`
}

type Asset struct {
	ID       string  `json:"id" jsonschema_description:"Asset identifier"`
	Name     string  `json:"name" jsonschema_description:"Asset name"`
	Value    float64 `json:"value" jsonschema_description:"Current value"`
	Currency string  `json:"currency" jsonschema_description:"Currency code"`
}

type AssetListRequest struct {
	Limit int `json:"limit,omitempty" jsonschema_description:"Number of assets to return"`
}

func main() {
	s := server.NewMCPServer(
		"Structured Output Example",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	// Example 1: Auto-generated schema from struct
	weatherTool := mcp.NewTool("get_weather",
		mcp.WithDescription("Get weather with structured output"),
		mcp.WithOutputSchema[WeatherResponse](),
		mcp.WithString("location", mcp.Required()),
		mcp.WithString("units", mcp.Enum("celsius", "fahrenheit"), mcp.DefaultString("celsius")),
	)
	s.AddTool(weatherTool, mcp.NewStructuredToolHandler(getWeatherHandler))

	// Example 2: Nested struct schema
	userTool := mcp.NewTool("get_user_profile",
		mcp.WithDescription("Get user profile"),
		mcp.WithOutputSchema[UserProfile](),
		mcp.WithString("userId", mcp.Required()),
	)
	s.AddTool(userTool, mcp.NewStructuredToolHandler(getUserProfileHandler))

	// Example 3: Array output - direct array of objects
	assetsTool := mcp.NewTool("get_assets",
		mcp.WithDescription("Get list of assets as array"),
		mcp.WithOutputSchema[[]Asset](),
		mcp.WithNumber("limit", mcp.Min(1), mcp.Max(100), mcp.DefaultNumber(10)),
	)
	s.AddTool(assetsTool, mcp.NewStructuredToolHandler(getAssetsHandler))

	// Example 4: Manual result creation
	manualTool := mcp.NewTool("manual_structured",
		mcp.WithDescription("Manual structured result"),
		mcp.WithOutputSchema[WeatherResponse](),
		mcp.WithString("location", mcp.Required()),
	)
	s.AddTool(manualTool, mcp.NewTypedToolHandler(manualWeatherHandler))

	if err := server.ServeStdio(s); err != nil {
		fmt.Printf("Server error: %v\n", err)
	}
}

func getWeatherHandler(ctx context.Context, request mcp.CallToolRequest, args WeatherRequest) (WeatherResponse, error) {
	temp := 22.5
	if args.Units == "fahrenheit" {
		temp = temp*9/5 + 32
	}

	return WeatherResponse{
		Location:    args.Location,
		Temperature: temp,
		Units:       args.Units,
		Conditions:  "Cloudy with a chance of meatballs",
		Timestamp:   time.Now(),
	}, nil
}

func getUserProfileHandler(ctx context.Context, request mcp.CallToolRequest, args UserRequest) (UserProfile, error) {
	return UserProfile{
		ID:    args.UserID,
		Name:  "John Doe",
		Email: "john.doe@example.com",
		Tags:  []string{"developer", "golang"},
	}, nil
}

func getAssetsHandler(ctx context.Context, request mcp.CallToolRequest, args AssetListRequest) ([]Asset, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = 10
	}

	assets := []Asset{
		{ID: "btc", Name: "Bitcoin", Value: 45000.50, Currency: "USD"},
		{ID: "eth", Name: "Ethereum", Value: 3200.75, Currency: "USD"},
		{ID: "ada", Name: "Cardano", Value: 0.85, Currency: "USD"},
		{ID: "sol", Name: "Solana", Value: 125.30, Currency: "USD"},
		{ID: "dot", Name: "Pottedot", Value: 18.45, Currency: "USD"},
	}

	if limit > len(assets) {
		limit = len(assets)
	}

	return assets[:limit], nil
}

func manualWeatherHandler(ctx context.Context, request mcp.CallToolRequest, args WeatherRequest) (*mcp.CallToolResult, error) {
	response := WeatherResponse{
		Location:    args.Location,
		Temperature: 25.0,
		Units:       "celsius",
		Conditions:  "Sunny, yesterday my life was filled with rain",
		Timestamp:   time.Now(),
	}

	fallbackText := fmt.Sprintf("Weather in %s: %.1f°C, %s",
		response.Location, response.Temperature, response.Conditions)

	return mcp.NewToolResultStructured(response, fallbackText), nil
}
