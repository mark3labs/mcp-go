package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
)

var (
	// DefaultLogger is the default logger instance
	DefaultLogger *Logger = NewLoggerWithAutoConfig(os.Getenv("LOG_CONFIG"))
)

// Logger encapsulates the slog logger
type Logger struct {
	logger *slog.Logger
}

// Config holds logger configuration
type Config struct {
	Level     string `json:"level"`      // Log level: "DEBUG", "INFO", "WARN", "ERROR"
	Format    string `json:"format"`     // Output format: "json", "text"
	AddSource bool   `json:"add_source"` // Whether to include source code location
}

// NewLogger creates a new logger instance with the given config
func NewLogger(config Config) *Logger {
	var level slog.Level
	switch config.Level {
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: config.AddSource,
	}

	var handler slog.Handler
	switch config.Format {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	default:
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return &Logger{
		logger: slog.New(handler),
	}
}

// NewLoggerWithAutoConfig creates a logger by trying config file, then environment variables, then defaulting to console
func NewLoggerWithAutoConfig(configPath string) *Logger {
	if config, err := loadFromFile(configPath); err == nil {
		return NewLogger(config)
	}
	if config, ok := loadFromEnv(); ok {
		return NewLogger(config)
	}
	return NewLogger(Config{
		Level:     "INFO",
		Format:    "text",
		AddSource: false,
	})
}

// loadFromFile attempts to load configuration from a JSON file
func loadFromFile(configPath string) (Config, error) {
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(configData, &config); err != nil {
		return Config{}, err
	}
	if config.Level == "" {
		config.Level = "INFO"
	}
	if config.Format == "" {
		config.Format = "text"
	}
	return config, nil
}

// loadFromEnv attempts to load configuration from environment variables
func loadFromEnv() (Config, bool) {
	config := Config{}
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		config.Level = level
	}
	if format := os.Getenv("LOG_FORMAT"); format != "" {
		config.Format = format
	}
	if addSource := os.Getenv("LOG_ADD_SOURCE"); addSource == "true" {
		config.AddSource = true
	}
	if config.Level == "" && config.Format == "" && !config.AddSource {
		return config, false
	}
	if config.Level == "" {
		config.Level = "INFO"
	}
	if config.Format == "" {
		config.Format = "text"
	}
	return config, true
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, args ...any) {
	l.logger.Debug(msg, args...)
}

// Debugf logs a formatted debug message
func (l *Logger) Debugf(format string, args ...any) {
	l.logger.Debug(fmt.Sprintf(format, args...))
}

// Info logs an info message
func (l *Logger) Info(msg string, args ...any) {
	l.logger.Info(msg, args...)
}

// Infof logs a formatted info message
func (l *Logger) Infof(format string, args ...any) {
	l.logger.Info(fmt.Sprintf(format, args...))
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)
}

// Warnf logs a formatted warning message
func (l *Logger) Warnf(format string, args ...any) {
	l.logger.Warn(fmt.Sprintf(format, args...))
}

// Error logs an error message
func (l *Logger) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)
}

// Errorf logs a formatted error message
func (l *Logger) Errorf(format string, args ...any) {
	l.logger.Error(fmt.Sprintf(format, args...))
}

// With adds context fields to the logger
func (l *Logger) With(args ...any) *Logger {
	return &Logger{
		logger: l.logger.With(args...),
	}
}

// WithContext adds context information from a context
func (l *Logger) WithContext(ctx context.Context) *Logger {
	return &Logger{
		logger: l.logger.With(slog.String("trace_id", getTraceID(ctx))),
	}
}

// getTraceID retrieves a trace ID from the context (example implementation)
func getTraceID(ctx context.Context) string {
	if traceID, ok := ctx.Value("trace_id").(string); ok {
		return traceID
	}
	return "no-trace-id"
}
