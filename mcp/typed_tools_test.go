package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTypedToolHandler(t *testing.T) {
	// Define a test struct for arguments
	type HelloArgs struct {
		Name    string `json:"name"`
		Age     int    `json:"age"`
		IsAdmin bool   `json:"is_admin"`
	}

	// Create a typed handler function
	typedHandler := func(ctx context.Context, request CallToolRequest, args HelloArgs) (*CallToolResult, error) {
		return NewToolResultText(args.Name), nil
	}

	// Create a wrapped handler
	wrappedHandler := NewTypedToolHandler(typedHandler)

	// Create a test request
	req := CallToolRequest{}
	req.Params.Name = "test-tool"
	req.Params.Arguments = map[string]any{
		"name":     "John Doe",
		"age":      30,
		"is_admin": true,
	}

	// Call the wrapped handler
	result, err := wrappedHandler(context.Background(), req)

	// Verify results
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "John Doe", result.Content[0].(TextContent).Text)

	// Test with invalid arguments
	req.Params.Arguments = map[string]any{
		"name":     123, // Wrong type
		"age":      "thirty",
		"is_admin": "yes",
	}

	// This should still work because of type conversion
	result, err = wrappedHandler(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Test with missing required field
	req.Params.Arguments = map[string]any{
		"age":      30,
		"is_admin": true,
		// Name is missing
	}

	// This should still work but name will be empty
	result, err = wrappedHandler(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "", result.Content[0].(TextContent).Text)

	// Test with completely invalid arguments
	req.Params.Arguments = "not a map"
	result, err = wrappedHandler(context.Background(), req)
	assert.NoError(t, err) // Error is wrapped in the result
	assert.NotNil(t, result)
	assert.True(t, result.IsError)
}

func TestTypedToolHandlerWithValidation(t *testing.T) {
	// Define a test struct for arguments with validation
	type CalculatorArgs struct {
		Operation string  `json:"operation"`
		X         float64 `json:"x"`
		Y         float64 `json:"y"`
	}

	// Create a typed handler function with validation
	typedHandler := func(ctx context.Context, request CallToolRequest, args CalculatorArgs) (*CallToolResult, error) {
		// Validate operation
		if args.Operation == "" {
			return NewToolResultError("operation is required"), nil
		}

		var result float64
		switch args.Operation {
		case "add":
			result = args.X + args.Y
		case "subtract":
			result = args.X - args.Y
		case "multiply":
			result = args.X * args.Y
		case "divide":
			if args.Y == 0 {
				return NewToolResultError("division by zero"), nil
			}
			result = args.X / args.Y
		default:
			return NewToolResultError("invalid operation"), nil
		}

		return NewToolResultText(fmt.Sprintf("%.0f", result)), nil
	}

	// Create a wrapped handler
	wrappedHandler := NewTypedToolHandler(typedHandler)

	// Create a test request
	req := CallToolRequest{}
	req.Params.Name = "calculator"
	req.Params.Arguments = map[string]any{
		"operation": "add",
		"x":         10.5,
		"y":         5.5,
	}

	// Call the wrapped handler
	result, err := wrappedHandler(context.Background(), req)

	// Verify results
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "16", result.Content[0].(TextContent).Text)

	// Test division by zero
	req.Params.Arguments = map[string]any{
		"operation": "divide",
		"x":         10.0,
		"y":         0.0,
	}

	result, err = wrappedHandler(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].(TextContent).Text, "division by zero")
}

func TestTypedToolHandlerWithComplexObjects(t *testing.T) {
	// Define a complex test struct with nested objects
	type Address struct {
		Street  string `json:"street"`
		City    string `json:"city"`
		Country string `json:"country"`
		ZipCode string `json:"zip_code"`
	}

	type UserPreferences struct {
		Theme       string   `json:"theme"`
		Timezone    string   `json:"timezone"`
		Newsletters []string `json:"newsletters"`
	}

	type UserProfile struct {
		Name        string         `json:"name"`
		Email       string         `json:"email"`
		Age         int            `json:"age"`
		IsVerified  bool           `json:"is_verified"`
		Address     Address        `json:"address"`
		Preferences UserPreferences `json:"preferences"`
		Tags        []string       `json:"tags"`
	}

	// Create a typed handler function
	typedHandler := func(ctx context.Context, request CallToolRequest, profile UserProfile) (*CallToolResult, error) {
		// Validate required fields
		if profile.Name == "" {
			return NewToolResultError("name is required"), nil
		}
		if profile.Email == "" {
			return NewToolResultError("email is required"), nil
		}

		// Build a response that includes nested object data
		response := fmt.Sprintf("User: %s (%s)", profile.Name, profile.Email)
		
		if profile.Age > 0 {
			response += fmt.Sprintf(", Age: %d", profile.Age)
		}
		
		if profile.IsVerified {
			response += ", Verified: Yes"
		} else {
			response += ", Verified: No"
		}
		
		// Include address information if available
		if profile.Address.City != "" && profile.Address.Country != "" {
			response += fmt.Sprintf(", Location: %s, %s", profile.Address.City, profile.Address.Country)
		}
		
		// Include preferences if available
		if profile.Preferences.Theme != "" {
			response += fmt.Sprintf(", Theme: %s", profile.Preferences.Theme)
		}
		
		if len(profile.Preferences.Newsletters) > 0 {
			response += fmt.Sprintf(", Subscribed to %d newsletters", len(profile.Preferences.Newsletters))
		}
		
		if len(profile.Tags) > 0 {
			response += fmt.Sprintf(", Tags: %v", profile.Tags)
		}
		
		return NewToolResultText(response), nil
	}

	// Create a wrapped handler
	wrappedHandler := NewTypedToolHandler(typedHandler)

	// Test with complete complex object
	req := CallToolRequest{}
	req.Params.Name = "user_profile"
	req.Params.Arguments = map[string]any{
		"name":        "John Doe",
		"email":       "john@example.com",
		"age":         35,
		"is_verified": true,
		"address": map[string]any{
			"street":   "123 Main St",
			"city":     "San Francisco",
			"country":  "USA",
			"zip_code": "94105",
		},
		"preferences": map[string]any{
			"theme":       "dark",
			"timezone":    "America/Los_Angeles",
			"newsletters": []string{"weekly", "product_updates"},
		},
		"tags": []string{"premium", "early_adopter"},
	}

	// Call the wrapped handler
	result, err := wrappedHandler(context.Background(), req)

	// Verify results
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Contains(t, result.Content[0].(TextContent).Text, "John Doe")
	assert.Contains(t, result.Content[0].(TextContent).Text, "San Francisco, USA")
	assert.Contains(t, result.Content[0].(TextContent).Text, "Theme: dark")
	assert.Contains(t, result.Content[0].(TextContent).Text, "Subscribed to 2 newsletters")
	assert.Contains(t, result.Content[0].(TextContent).Text, "Tags: [premium early_adopter]")

	// Test with partial data (missing some nested fields)
	req.Params.Arguments = map[string]any{
		"name":        "Jane Smith",
		"email":       "jane@example.com",
		"age":         28,
		"is_verified": false,
		"address": map[string]any{
			"city":    "London",
			"country": "UK",
		},
		"preferences": map[string]any{
			"theme": "light",
		},
	}

	result, err = wrappedHandler(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Contains(t, result.Content[0].(TextContent).Text, "Jane Smith")
	assert.Contains(t, result.Content[0].(TextContent).Text, "London, UK")
	assert.Contains(t, result.Content[0].(TextContent).Text, "Theme: light")
	assert.NotContains(t, result.Content[0].(TextContent).Text, "newsletters")

	// Test with JSON string input (simulating raw JSON from client)
	jsonInput := `{
		"name": "Bob Johnson",
		"email": "bob@example.com",
		"age": 42,
		"is_verified": true,
		"address": {
			"street": "456 Park Ave",
			"city": "New York",
			"country": "USA",
			"zip_code": "10022"
		},
		"preferences": {
			"theme": "system",
			"timezone": "America/New_York",
			"newsletters": ["monthly"]
		},
		"tags": ["business"]
	}`

	req.Params.Arguments = json.RawMessage(jsonInput)
	result, err = wrappedHandler(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Contains(t, result.Content[0].(TextContent).Text, "Bob Johnson")
	assert.Contains(t, result.Content[0].(TextContent).Text, "New York, USA")
	assert.Contains(t, result.Content[0].(TextContent).Text, "Theme: system")
	assert.Contains(t, result.Content[0].(TextContent).Text, "Subscribed to 1 newsletters")
}