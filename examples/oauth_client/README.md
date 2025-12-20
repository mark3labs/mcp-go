# OAuth Client Example

This example demonstrates how to use the OAuth capabilities of the MCP Go client to authenticate with an MCP server that requires OAuth authentication, specifically configured for GitHub.

## Features

- OAuth 2.0 authentication
- Preset configuration for GitHub (`client.GitHubProvider`)
- Authorization code flow
- Token refresh
- Local callback server for handling OAuth redirects

## Standard vs Manual Provider Configuration

By default, the client uses **RFC 8414** to automatically discover the authorization server metadata from the MCP server's base URL. This works for most standard OAuth implementations.

However, some providers (like GitHub) do not support RFC 8414 or host their OAuth endpoints on a different domain than their API. For these cases, you can provide an optional `Provider` configuration to manually specify the endpoints and capabilities.

This example uses the manual `GitHubProvider` configuration. For standard providers, you can omit the `Provider` field and `PKCEEnabled` is `true` by default.

## Usage

```bash
# Set environment variables (optional)
export MCP_CLIENT_ID=your_client_id
export MCP_CLIENT_SECRET=your_client_secret

# Run the example
go run main.go
```

## How it Works

1. The example initializes the client with `client.GitHubProvider` to bypass incorrect auto-discovery for GitHub.
2. It uses `read:user` scope as required by GitHub.
3. PKCE is disabled as it is not supported by GitHub's OAuth implementation.
4. The client starts the OAuth flow:
   - Generates a state parameter for security
   - Opens a browser to the GitHub authorization URL
   - Starts a local server to handle the callback at `http://localhost:8085/oauth/callback`
5. The user authorizes the application in their browser.
6. GitHub redirects back to the local callback server.
7. The client exchanges the authorization code for an access token.
8. The client initializes the connection to the MCP server with the authenticated token.

## Configuration

Edit the following constants in `main.go` to match your environment:

```go
const (
    // Replace with your MCP server URL
    serverURL = "https://api.githubcopilot.com/v1/mcp"
    // Use a localhost redirect URI for this example
    redirectURI = "http://localhost:8085/oauth/callback"
)
```

**Note:** Ensure your GitHub OAuth App is configured with `http://localhost:8085/oauth/callback` as the Authorization callback URL.

## OAuth Scopes

The example requests the following scope:

- `read:user` - Read access to user profile (minimal scope for testing)

You can modify the scopes in the `oauthConfig` to match the requirements of your specific application or MCP server.