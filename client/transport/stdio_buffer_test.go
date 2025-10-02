package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// writeCloser wraps a bytes.Buffer to implement io.WriteCloser
type writeCloser struct {
	*bytes.Buffer
}

func (wc *writeCloser) Close() error {
	return nil
}

func TestStdio_BufferSizeConfiguration(t *testing.T) {
	tests := []struct {
		name           string
		bufferSize     int
		expectedBuffer int
	}{
		{
			name:           "Default buffer size",
			bufferSize:     0,
			expectedBuffer: DefaultBufferSize,
		},
		{
			name:           "Custom buffer size",
			bufferSize:     512 * 1024, // 512KB
			expectedBuffer: 512 * 1024,
		},
		{
			name:           "Large buffer size",
			bufferSize:     10 * 1024 * 1024, // 10MB
			expectedBuffer: 10 * 1024 * 1024,
		},
		{
			name:           "Negative buffer size (should use default)",
			bufferSize:     -1,
			expectedBuffer: DefaultBufferSize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var client *Stdio
			if tt.bufferSize != 0 {
				client = NewStdioWithOptions("echo", nil, nil, WithBufferSize(tt.bufferSize))
			} else {
				client = NewStdioWithOptions("echo", nil, nil)
			}

			if client.GetBufferSize() != tt.expectedBuffer {
				t.Errorf("Expected buffer size %d, got %d", tt.expectedBuffer, client.GetBufferSize())
			}
		})
	}
}

func TestStdio_SetBufferSize(t *testing.T) {
	client := NewStdioWithOptions("echo", nil, nil)
	
	// Test setting valid buffer size
	client.SetBufferSize(2 * 1024 * 1024) // 2MB
	if client.GetBufferSize() != 2*1024*1024 {
		t.Errorf("Expected buffer size %d, got %d", 2*1024*1024, client.GetBufferSize())
	}
	
	// Test setting invalid buffer size (should use default)
	client.SetBufferSize(-100)
	if client.GetBufferSize() != DefaultBufferSize {
		t.Errorf("Expected buffer size %d, got %d", DefaultBufferSize, client.GetBufferSize())
	}
	
	// Test setting zero buffer size (should use default)
	client.SetBufferSize(0)
	if client.GetBufferSize() != DefaultBufferSize {
		t.Errorf("Expected buffer size %d, got %d", DefaultBufferSize, client.GetBufferSize())
	}
}

func TestStdio_LargeMessageHandling(t *testing.T) {
	tests := []struct {
		name       string
		dataSize   int
		bufferSize int
		shouldPass bool
	}{
		{
			name:       "Small message with default buffer",
			dataSize:   1024,        // 1KB
			bufferSize: 0,           // Use default
			shouldPass: true,
		},
		{
			name:       "Medium message with default buffer",
			dataSize:   64 * 1024,   // 64KB (old scanner limit)
			bufferSize: 0,           // Use default (1MB)
			shouldPass: true,
		},
		{
			name:       "Large message with default buffer",
			dataSize:   512 * 1024,  // 512KB
			bufferSize: 0,           // Use default (1MB)
			shouldPass: true,
		},
		{
			name:       "Very large message with custom buffer",
			dataSize:   5 * 1024 * 1024,  // 5MB
			bufferSize: 8 * 1024 * 1024,  // 8MB buffer
			shouldPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a large JSON-RPC response
			largeData := generateTestString(tt.dataSize)
			response := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"result": map[string]interface{}{
					"content": largeData,
					"size":    len(largeData),
				},
			}

			responseBytes, err := json.Marshal(response)
			if err != nil {
				t.Fatalf("Failed to marshal response: %v", err)
			}
			responseStr := string(responseBytes) + "\n"

			// Create stdio client with test data
			input := strings.NewReader(responseStr)
			output := &writeCloser{&bytes.Buffer{}}
			stderr := &bytes.Buffer{}

			var client *Stdio
			if tt.bufferSize > 0 {
				client = NewIO(input, output, io.NopCloser(stderr))
				client.SetBufferSize(tt.bufferSize)
			} else {
				client = NewIO(input, output, io.NopCloser(stderr))
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Start the client
			err = client.Start(ctx)
			if tt.shouldPass {
				if err != nil {
					t.Errorf("Expected success but got error: %v", err)
				}
			}

			client.Close()
			
			t.Logf("Successfully handled message of size %d bytes with buffer size %d", 
				tt.dataSize, client.GetBufferSize())
		})
	}
}

func TestStdio_NewIOWithCustomBuffer(t *testing.T) {
	largeData := generateTestString(2 * 1024 * 1024) // 2MB
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"result":  map[string]interface{}{"content": largeData},
	}
	
	responseBytes, _ := json.Marshal(response)
	responseStr := string(responseBytes) + "\n"
	
	input := strings.NewReader(responseStr)
	output := &writeCloser{&bytes.Buffer{}}
	stderr := &bytes.Buffer{}
	
	// Test that NewIO uses DefaultBufferSize
	client := NewIO(input, output, io.NopCloser(stderr))
	if client.GetBufferSize() != DefaultBufferSize {
		t.Errorf("Expected NewIO to use DefaultBufferSize %d, got %d", 
			DefaultBufferSize, client.GetBufferSize())
	}
}

func BenchmarkStdio_LargeMessageHandling(b *testing.B) {
	sizes := []int{1024, 64 * 1024, 512 * 1024, 2 * 1024 * 1024}
	
	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size_%dKB", size/1024), func(b *testing.B) {
			largeData := generateTestString(size)
			response := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  map[string]interface{}{"content": largeData},
			}
			
			responseBytes, _ := json.Marshal(response)
			responseStr := string(responseBytes) + "\n"
			
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				input := strings.NewReader(responseStr)
				output := &writeCloser{&bytes.Buffer{}}
				stderr := &bytes.Buffer{}
				
				client := NewIO(input, output, io.NopCloser(stderr))
				client.Start(context.Background())
				client.Close()
			}
		})
	}
}

func BenchmarkStdio_BufferSizes(b *testing.B) {
	bufferSizes := []int{4 * 1024, 64 * 1024, 1024 * 1024, 4 * 1024 * 1024}
	messageSize := 512 * 1024 // 512KB message
	
	largeData := generateTestString(messageSize)
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"result":  map[string]interface{}{"content": largeData},
	}
	
	responseBytes, _ := json.Marshal(response)
	responseStr := string(responseBytes) + "\n"
	
	for _, bufSize := range bufferSizes {
		b.Run(fmt.Sprintf("Buffer_%dKB", bufSize/1024), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				input := strings.NewReader(responseStr)
				output := &writeCloser{&bytes.Buffer{}}
				stderr := &bytes.Buffer{}
				
				client := NewIO(input, output, io.NopCloser(stderr))
				client.SetBufferSize(bufSize)
				client.Start(context.Background())
				client.Close()
			}
		})
	}
}

// generateTestString creates a test string of the specified size
func generateTestString(size int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "
	
	b := make([]byte, size)
	for i := range b {
		b[i] = charset[i%len(charset)]
	}
	return string(b)
}