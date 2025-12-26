// Package main is the entry point for the Invarity Firewall server.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"invarity/internal/audit"
	"invarity/internal/config"
	"invarity/internal/firewall"
	invarhttp "invarity/internal/http"
	"invarity/internal/llm"
	"invarity/internal/registry"
)

const (
	version = "0.1.0"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize logger
	logger, err := initLogger(cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("failed to init logger: %w", err)
	}
	defer logger.Sync()

	logger.Info("starting Invarity Firewall",
		zap.String("version", version),
		zap.Int("port", cfg.Port),
	)

	// Initialize stores
	registryStore := registry.NewInMemoryStoreWithDefaults()
	auditStore := audit.NewInMemoryStore()

	// Initialize LLM clients
	alignmentClient := llm.NewClient(llm.ClientConfig{
		BaseURL: cfg.FunctionGemmaBaseURL,
		APIKey:  cfg.FunctionGemmaAPIKey,
		Model:   "functiongemma",
		Timeout: 30 * time.Second,
	})

	threatClient := llm.NewClient(llm.ClientConfig{
		BaseURL: cfg.LlamaGuardBaseURL,
		APIKey:  cfg.LlamaGuardAPIKey,
		Model:   "llama-guard-3",
		Timeout: 30 * time.Second,
	})

	// Initialize pipeline
	pipeline := firewall.NewPipeline(firewall.PipelineConfig{
		Config:          cfg,
		Logger:          logger,
		RegistryStore:   registryStore,
		AuditStore:      auditStore,
		AlignmentClient: alignmentClient,
		ThreatClient:    threatClient,
	})

	// Initialize router
	router := invarhttp.NewRouter(invarhttp.RouterConfig{
		Logger:   logger,
		Pipeline: pipeline,
	})

	// Create server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server listening", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case sig := <-quit:
		logger.Info("shutting down", zap.String("signal", sig.String()))
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	logger.Info("server stopped")
	return nil
}

func initLogger(level string) (*zap.Logger, error) {
	var zapLevel zapcore.Level
	switch level {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	config := zap.Config{
		Level:       zap.NewAtomicLevelAt(zapLevel),
		Development: false,
		Encoding:    "json",
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "timestamp",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			FunctionKey:    zapcore.OmitKey,
			MessageKey:     "message",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.MillisDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		},
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	return config.Build()
}
