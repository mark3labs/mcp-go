package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestRegistryReplacementDoesNotExposeEmptyIntermediateState(t *testing.T) {
	for _, kind := range []string{"resources", "templates", "prompts"} {
		t.Run(kind, func(t *testing.T) {
			s := NewMCPServer("test", "1")
			set := func(name string) {
				switch kind {
				case "resources":
					s.SetResources(ServerResource{Resource: mcp.NewResource("file:///"+name, name)})
				case "templates":
					s.SetResourceTemplates(ServerResourceTemplate{Template: mcp.NewResourceTemplate("file:///"+name+"/{id}", name)})
				default:
					s.SetPrompts(ServerPrompt{Prompt: mcp.NewPrompt(name)})
				}
			}
			count := func() int {
				if kind == "prompts" {
					return len(s.ListPrompts())
				}
				s.resourcesMu.RLock()
				defer s.resourcesMu.RUnlock()
				if kind == "templates" {
					return len(s.resourceTemplates)
				}
				return len(s.resources)
			}
			set("old")
			// Every setter must reach registration before checking the old registry.
			// The held lock then keeps the replacement at that exact checkpoint.
			s.capabilitiesMu.Lock()
			reached := make(chan struct{})
			done := make(chan struct{})
			s.beforeCapabilityRegistration = func() { close(reached) }
			locked := true
			t.Cleanup(func() {
				if locked {
					s.capabilitiesMu.Unlock()
				}
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Error("replacement goroutine did not finish after unlocking")
				}
			})
			go func() { set("new"); close(done) }()
			select {
			case <-reached:
			case <-time.After(2 * time.Second):
				t.Fatal("replacement did not reach capability registration")
			}
			require.Equal(t, 1, count(), "replacement exposed an empty registry between clearing and adding")
			s.capabilitiesMu.Unlock()
			locked = false
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("replacement did not finish")
			}
			require.Equal(t, 1, count())
		})
	}
}
