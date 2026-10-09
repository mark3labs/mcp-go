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
			// Pause capability registration, which every replacement performs. The
			// existing registry must remain visible until the replacement is ready.
			s.capabilitiesMu.Lock()
			done := make(chan struct{})
			go func() { set("new"); close(done) }()
			empty := false
			deadline := time.After(50 * time.Millisecond)
		loop:
			for {
				if count() == 0 {
					empty = true
					break
				}
				select {
				case <-deadline:
					break loop
				default:
					time.Sleep(time.Millisecond)
				}
			}
			s.capabilitiesMu.Unlock()
			<-done
			require.False(t, empty, "replacement exposed an empty registry between clearing and adding")
			require.Equal(t, 1, count())
		})
	}
}
