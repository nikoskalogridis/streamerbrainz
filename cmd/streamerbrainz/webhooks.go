package main

import (
	"fmt"
	"log/slog"
	"net/http"
)

// ============================================================================
// Webhooks Server
// ============================================================================
// Generic HTTP server for receiving webhook events from external services.
// Individual integrations register their own endpoints.
// ============================================================================

// runWebhooksServer starts the HTTP server on the specified port
func runWebhooksServer(port int, logger *slog.Logger) error {
	listenAddr := fmt.Sprintf(":%d", port)
	logger.Info("webhooks server listening", "port", port)

	if err := http.ListenAndServe(listenAddr, nil); err != nil {
		return fmt.Errorf("HTTP server: %w", err)
	}

	return nil
}
