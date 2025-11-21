package server

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestStreamableHttpSessionImplementsSessionWithClientInfo(t *testing.T) {
	// Create the session stores
	toolStore := newSessionToolsStore()
	resourceStore := newSessionResourcesStore()
	templatesStore := newSessionResourceTemplatesStore()
	logStore := newSessionLogLevelsStore()
	
	// Create a streamable HTTP session
	session := newStreamableHttpSession("test-session", toolStore, resourceStore, templatesStore, logStore)
	
	// Verify it implements SessionWithClientInfo
	var clientSession ClientSession = session
	clientInfoSession, ok := clientSession.(SessionWithClientInfo)
	if !ok {
		t.Fatal("streamableHttpSession should implement SessionWithClientInfo")
	}
	
	// Test GetClientInfo with no data set (should return empty)
	clientInfo := clientInfoSession.GetClientInfo()
	if clientInfo.Name != "" || clientInfo.Version != "" {
		t.Errorf("expected empty client info, got %+v", clientInfo)
	}
	
	// Test SetClientInfo and GetClientInfo
	expectedClientInfo := mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}
	clientInfoSession.SetClientInfo(expectedClientInfo)
	
	actualClientInfo := clientInfoSession.GetClientInfo()
	if actualClientInfo.Name != expectedClientInfo.Name || actualClientInfo.Version != expectedClientInfo.Version {
		t.Errorf("expected client info %+v, got %+v", expectedClientInfo, actualClientInfo)
	}
	
	// Test GetClientCapabilities with no data set (should return empty)
	capabilities := clientInfoSession.GetClientCapabilities()
	if capabilities.Sampling != nil || capabilities.Roots != nil {
		t.Errorf("expected empty client capabilities, got %+v", capabilities)
	}
	
	// Test SetClientCapabilities and GetClientCapabilities
	expectedCapabilities := mcp.ClientCapabilities{
		Sampling: &struct{}{},
	}
	clientInfoSession.SetClientCapabilities(expectedCapabilities)
	
	actualCapabilities := clientInfoSession.GetClientCapabilities()
	if actualCapabilities.Sampling == nil {
		t.Errorf("expected sampling capability to be set")
	}
}