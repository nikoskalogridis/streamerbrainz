package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// ============================================================================
// Webhooks Server
// ============================================================================
// Generic HTTP server for receiving webhook events from external services.
// Individual integrations register their own endpoints.
// ============================================================================

// runWebhooksServer starts the HTTP server on the specified port and shuts it down
// gracefully when ctx is canceled.
//
// This replaces http.ListenAndServe so we can call Server.Shutdown during program shutdown.
func runWebhooksServer(ctx context.Context, port int, logger *slog.Logger) error {
	listenAddr := fmt.Sprintf(":%d", port)
	logger.Info("webhooks server listening", "port", port)

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: http.DefaultServeMux, // handlers are registered globally elsewhere
	}

	errCh := make(chan error, 1)

	go func() {
		// ListenAndServe returns http.ErrServerClosed on Shutdown; treat that as clean exit.
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("HTTP server: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		// Graceful shutdown with a timeout.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("HTTP server shutdown: %w", err)
		}
		// Wait for the ListenAndServe goroutine to return.
		_ = <-errCh
		return nil

	case err := <-errCh:
		return err
	}
}
