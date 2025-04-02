package main

import (
	"context"
	"github.com/Hirocloud/mcp-go/server"
	"github.com/redis/go-redis/v9"
	"sync"
)

func main() {
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		startServer(":8080")
		wg.Done()
	}()
	go func() {
		startServer(":8081")
		wg.Done()
	}()
	wg.Wait()
}

func startServer(port string) {
	mcpServer := server.NewMCPServer(
		"example-servers/everything",
		"1.0.0",
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
		server.WithLogging(),
	)
	r := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	sseServer := server.NewMCSSEServer(mcpServer, r)
	sseServer.CleanAuto(context.Background())
	sseServer.Start(port)
}
