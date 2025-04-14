package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// TestNewLogger tests the basic logger creation with a given config
func TestNewLogger(t *testing.T) {
	config := Config{
		Level:     "DEBUG",
		Format:    "text",
		AddSource: false,
	}
	log := NewLogger(config)

	if log == nil {
		t.Fatal("NewLogger returned nil")
	}

	// Redirect stdout to capture output
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	log.logger = slog.New(handler)

	// Test Debug logging
	log.Debug("test message", "key", "value")
	output := buf.String()
	if !strings.Contains(output, "level=DEBUG") || !strings.Contains(output, "msg=\"test message\"") || !strings.Contains(output, "key=value") {
		t.Errorf("Debug output missing expected content: %s", output)
	}
}

// TestNewLoggerWithAutoConfigFile tests loading from a config file
func TestNewLoggerWithAutoConfigFile(t *testing.T) {
	// Create a temporary config file
	config := Config{
		Level:     "INFO",
		Format:    "json",
		AddSource: true,
	}
	configData, _ := json.Marshal(config)
	tmpFile, err := os.CreateTemp("", "logger_test_*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write(configData); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	log := NewLoggerWithAutoConfig(tmpFile.Name())
	if log == nil {
		t.Fatal("NewLoggerWithAutoConfig returned nil")
	}

	// Redirect stdout to capture output
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo, AddSource: true})
	log.logger = slog.New(handler)

	// Test Info logging
	log.Info("test info")
	output := buf.String()
	if !strings.Contains(output, `"level":"INFO"`) || !strings.Contains(output, `"msg":"test info"`) || !strings.Contains(output, `"source":`) {
		t.Errorf("Info output missing expected content: %s", output)
	}
}

// TestNewLoggerWithAutoConfigEnv tests loading from environment variables
func TestNewLoggerWithAutoConfigEnv(t *testing.T) {
	// Set environment variables
	os.Setenv("LOG_LEVEL", "WARN")
	os.Setenv("LOG_FORMAT", "text")
	os.Setenv("LOG_ADD_SOURCE", "true")
	defer func() {
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("LOG_FORMAT")
		os.Unsetenv("LOG_ADD_SOURCE")
	}()

	log := NewLoggerWithAutoConfig("nonexistent.json") // File won't exist, should use env
	if log == nil {
		t.Fatal("NewLoggerWithAutoConfig returned nil")
	}

	// Redirect stdout to capture output
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn, AddSource: true})
	log.logger = slog.New(handler)

	// Test Warn logging
	log.Warn("test warning")
	output := buf.String()
	if !strings.Contains(output, "level=WARN") || !strings.Contains(output, "msg=\"test warning\"") || !strings.Contains(output, "source=") {
		t.Errorf("Warn output missing expected content: %s", output)
	}
}

// TestNewLoggerWithAutoConfigDefault tests the default configuration
func TestNewLoggerWithAutoConfigDefault(t *testing.T) {
	log := NewLoggerWithAutoConfig("nonexistent.json") // No file, no env vars
	if log == nil {
		t.Fatal("NewLoggerWithAutoConfig returned nil")
	}

	// Redirect stdout to capture output
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	log.logger = slog.New(handler)

	// Test Info logging (DEBUG should not appear)
	log.Info("test info")
	log.Debug("test debug") // Should not log due to INFO level
	output := buf.String()
	if !strings.Contains(output, "level=INFO") || !strings.Contains(output, "msg=\"test info\"") {
		t.Errorf("Info output missing expected content: %s", output)
	}
	if strings.Contains(output, "test debug") {
		t.Errorf("Debug message should not appear with INFO level: %s", output)
	}
}

// TestFormattedMethods tests the formatted logging methods
func TestFormattedMethods(t *testing.T) {
	config := Config{
		Level:     "DEBUG",
		Format:    "text",
		AddSource: false,
	}
	log := NewLogger(config)

	// Redirect stdout to capture output
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	log.logger = slog.New(handler)

	// Test formatted methods
	log.Debugf("User %s logged in", "alice")
	log.Infof("Server on port %d", 8080)
	log.Warnf("Request took %d ms", 150)
	log.Errorf("Error: %v", "timeout")

	output := buf.String()
	if !strings.Contains(output, "level=DEBUG msg=\"User alice logged in\"") {
		t.Errorf("Debugf output incorrect: %s", output)
	}
	if !strings.Contains(output, "level=INFO msg=\"Server on port 8080\"") {
		t.Errorf("Infof output incorrect: %s", output)
	}
	if !strings.Contains(output, "level=WARN msg=\"Request took 150 ms\"") {
		t.Errorf("Warnf output incorrect: %s", output)
	}
	if !strings.Contains(output, "level=ERROR msg=\"Error: timeout\"") {
		t.Errorf("Errorf output incorrect: %s", output)
	}
}

// TestWithContext tests logging with context
func TestWithContext(t *testing.T) {
	config := Config{
		Level:     "INFO",
		Format:    "json",
		AddSource: false,
	}
	log := NewLogger(config)

	// Redirect stdout to capture output
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	log.logger = slog.New(handler)

	// Test with context
	ctx := context.WithValue(context.Background(), "trace_id", "abc123")
	logWithCtx := log.WithContext(ctx)
	logWithCtx.Info("test with context")

	output := buf.String()
	if !strings.Contains(output, `"trace_id":"abc123"`) || !strings.Contains(output, `"msg":"test with context"`) {
		t.Errorf("Context output missing expected content: %s", output)
	}
}

// TestLogOutputStyles tests and displays various log output styles
func TestLogOutputStyles(t *testing.T) {
	// Test with Text format
	t.Run("Text Format", func(t *testing.T) {
		config := Config{
			Level:     "DEBUG",
			Format:    "text",
			AddSource: true,
		}
		log := NewLogger(config)

		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true})
		log.logger = slog.New(handler)

		// Log various levels
		log.Debug("Debug message", "key", "value")
		log.Info("Info message", "port", 8080)
		log.Warn("Warn message")
		log.Error("Error message", "error", "timeout")
		log.Debugf("Formatted debug: %s", "test")
		log.Infof("Formatted info: %d users", 5)

		t.Logf("Text Format Output:\n%s", buf.String())
	})

	// Test with JSON format
	t.Run("JSON Format", func(t *testing.T) {
		config := Config{
			Level:     "DEBUG",
			Format:    "json",
			AddSource: true,
		}
		log := NewLogger(config)

		var buf bytes.Buffer
		handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true})
		log.logger = slog.New(handler)

		// Log various levels
		log.Debug("Debug message", "key", "value")
		log.Info("Info message", "port", 8080)
		log.Warn("Warn message")
		log.Error("Error message", "error", "timeout")
		log.Debugf("Formatted debug: %s", "test")
		log.Infof("Formatted info: %d users", 5)

		t.Logf("JSON Format Output:\n%s", buf.String())
	})
}
