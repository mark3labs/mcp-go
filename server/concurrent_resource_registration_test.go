package server_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

func TestConcurrentSessionResourceRegistration(t *testing.T) {
	s := server.NewMCPServer("test", "1", server.WithResourceCapabilities(false, false))
	endpoint := httptest.NewServer(server.NewStreamableHTTPServer(s, server.WithStateful(true)))
	defer endpoint.Close()
	tr, err := transport.NewStreamableHTTP(endpoint.URL)
	require.NoError(t, err)
	c := client.NewClient(tr, client.WithProtocolVersion(mcp.LATEST_LEGACY_PROTOCOL_VERSION))
	defer c.Close()
	ctx := t.Context()
	require.NoError(t, c.Start(ctx))
	_, err = c.Initialize(ctx, mcp.InitializeRequest{})
	require.NoError(t, err)
	id := c.GetSessionId()
	require.NotEmpty(t, id)
	handlerFor := func(registeredURI string) server.ResourceHandlerFunc {
		return func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{mcp.TextResourceContents{URI: req.Params.URI, Text: registeredURI}}, nil
		}
	}
	const count = 64
	start := make(chan struct{})
	var writers sync.WaitGroup
	for i := range count {
		writers.Go(func() {
			<-start
			uri := fmt.Sprintf("test://items/%d", i)
			if err := s.AddSessionResource(id, mcp.NewResource(uri, "item"), handlerFor(uri)); err != nil {
				t.Error(err)
			}
			template := uri + "/{key}"
			if err := s.AddSessionResourceTemplate(id, mcp.NewResourceTemplate(template, "entry"), server.ResourceTemplateHandlerFunc(handlerFor(template))); err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	writers.Wait()
	resources, err := c.ListResources(ctx, mcp.ListResourcesRequest{})
	require.NoError(t, err)
	templates, err := c.ListResourceTemplates(ctx, mcp.ListResourceTemplatesRequest{})
	require.NoError(t, err)
	if got := len(resources.Resources); got != count {
		t.Errorf("listed %d resources, want %d", got, count)
	}
	if got := len(templates.ResourceTemplates); got != count {
		t.Errorf("listed %d templates, want %d", got, count)
	}

	for i := range count {
		uri := fmt.Sprintf("test://items/%d", i)
		for _, read := range []struct{ uri, registered string }{{uri, uri}, {uri + "/sample", uri + "/{key}"}} {
			request := mcp.ReadResourceRequest{}
			request.Params.URI = read.uri
			result, err := c.ReadResource(ctx, request)
			require.NoError(t, err, read.uri)
			require.Len(t, result.Contents, 1)
			content, ok := mcp.AsTextResourceContents(result.Contents[0])
			require.True(t, ok)
			require.Equal(t, read.uri, content.URI)
			require.Equal(t, read.registered, content.Text)
		}
	}

	start = make(chan struct{})
	for i := range count {
		writers.Go(func() {
			<-start
			uri := fmt.Sprintf("test://items/%d", i)
			if err := s.DeleteSessionResources(id, uri); err != nil {
				t.Error(err)
			}
			if err := s.DeleteSessionResourceTemplates(id, uri+"/{key}"); err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	writers.Wait()
	resources, err = c.ListResources(ctx, mcp.ListResourcesRequest{})
	require.NoError(t, err)
	require.Empty(t, resources.Resources)
	templates, err = c.ListResourceTemplates(ctx, mcp.ListResourceTemplatesRequest{})
	require.NoError(t, err)
	require.Empty(t, templates.ResourceTemplates)
}
