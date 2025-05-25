# OAuth Client Example

This example demonstrates how to use the OAuth capabilities of the MCP Go client to authenticate with an MCP server that requires OAuth authentication.

## Features

- OAuth 2.1 authentication with PKCE support
- Dynamic client registration
- Authorization code flow
- Token refresh
- Local callback server for handling OAuth redirects

## Usage

```bash
# Set environment variables (optional)
export MCP_CLIENT_ID=your_client_id
export MCP_CLIENT_SECRET=your_client_secret

# Run the example
go run main.go
```

## How it Works

1. The client attempts to initialize a connection to the MCP server
2. If the server requires OAuth authentication, it will return a 401 Unauthorized response
3. The client detects this and starts the OAuth flow:
   - Generates PKCE code verifier and challenge
   - Generates a state parameter for security
   - Opens a browser to the authorization URL
   - Starts a local server to handle the callback
4. The user authorizes the application in their browser
5. The authorization server redirects back to the local callback server
6. The client exchanges the authorization code for an access token
7. The client retries the initialization with the access token
8. The client can now make authenticated requests to the MCP server

## Configuration

Edit the following constants in `main.go` to match your environment:

```go
const (
    // Replace with your MCP server URL
    serverURL = "https://api.example.com/v1/mcp"
    // Use a localhost redirect URI for this example
    redirectURI = "http://localhost:8085/oauth/callback"
)
```

## OAuth Scopes

The example requests the following scopes:

- `mcp.read` - Read access to MCP resources
- `mcp.write` - Write access to MCP resources

You can modify the scopes in the `oauthConfig` to match the requirements of your MCP server.