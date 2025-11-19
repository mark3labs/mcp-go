# OAuth Client Example

This example demonstrates how to use the OAuth capabilities of the MCP Go client to authenticate with an MCP server that requires OAuth authentication.

## Features

- OAuth 2.1 authentication with PKCE support
- **RFC9728 OAuth Protected Resource Metadata discovery** - Automatically discovers OAuth server metadata from `WWW-Authenticate` headers
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
3. **The client automatically extracts the OAuth metadata URL from the `WWW-Authenticate` header** (per [RFC9728](https://datatracker.ietf.org/doc/html/rfc9728))
   - Example header: `WWW-Authenticate: Bearer resource_metadata="https://auth.example.com/.well-known/oauth-protected-resource"`
   - This URL tells the client where to find the OAuth authorization server configuration
4. The client detects the OAuth requirement and starts the OAuth flow:
   - Generates PKCE code verifier and challenge
   - Generates a state parameter for security
   - Opens a browser to the authorization URL
   - Starts a local server to handle the callback
5. The user authorizes the application in their browser
6. The authorization server redirects back to the local callback server
7. The client exchanges the authorization code for an access token
8. The client retries the initialization with the access token
9. The client can now make authenticated requests to the MCP server

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

## RFC9728 OAuth Protected Resource Metadata

This example demonstrates automatic OAuth metadata discovery per [RFC9728](https://datatracker.ietf.org/doc/html/rfc9728). When the MCP server returns a 401 Unauthorized response with a `WWW-Authenticate` header containing the `resource_metadata` parameter, the client automatically extracts and uses this URL to discover the OAuth authorization server configuration.

The example code demonstrates this with:

```go
// Check if server provided OAuth metadata URL via WWW-Authenticate header (RFC9728)
if metadataURL := client.GetDiscoveredMetadataURL(err); metadataURL != "" {
    fmt.Printf("Server provided OAuth metadata URL: %s\n", metadataURL)
}
```

This makes the OAuth flow more robust and standards-compliant, as the server explicitly tells clients where to find OAuth configuration rather than relying on well-known locations.