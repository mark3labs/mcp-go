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

	// Example 3: Manual result creation
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
