package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"git.seasonalnet.org/SeasonalNet/seasonalnet-icon-cdn/internal/cdn"
)

func main() {
	if len(os.Args) > 1 {
		if os.Args[1] == "healthcheck" {
			runHealthcheck()
			return
		}
		log.Fatalf("unknown command %q", os.Args[1])
	}
	appRoot, err := os.Getwd()
	if err != nil {
		log.Fatalf("[cdn] resolve working directory: %v", err)
	}
	config, err := cdn.LoadConfig(appRoot, processEnvironment())
	if err != nil {
		log.Fatalf("[cdn] FATAL: config error: %v", err)
	}
	renderer, err := cdn.NewRenderer(config.Render.IconsDir)
	if err != nil {
		log.Fatalf("[cdn] FATAL: renderer initialization: %v", err)
	}
	defer func() {
		if err := renderer.Close(); err != nil {
			log.Printf("[cdn] renderer shutdown error: %v", err)
		}
	}()
	defer cdn.ShutdownImageEngine()
	service, err := cdn.NewServer(config, renderer)
	if err != nil {
		log.Fatalf("[cdn] startup error: %v", err)
	}
	defer service.Close()

	server := &http.Server{
		Addr:              net.JoinHostPort(config.Server.Host, strconv.Itoa(config.Server.Port)),
		Handler:           service.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("[cdn] SeasonalNet icon CDN listening on %s:%d", config.Server.Host, config.Server.Port)
		log.Printf("[cdn] Config: %s", config.ConfigPath)
		log.Printf("[cdn] Icons: Lucide %s (%d icons)", renderer.Version(), len(renderer.IconNames()))
		log.Printf("[cdn] Cache dir: %s", config.Cache.VersionedDir)
		log.Printf("[cdn] Size: %dpx", config.Render.Size)
		serverErrors <- server.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[cdn] server error: %v", err)
		}
	case <-ctx.Done():
		service.Close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("[cdn] graceful shutdown failed: %v", err)
			_ = server.Close()
		}
	}
}

func runHealthcheck() {
	client := &http.Client{Timeout: 5 * time.Second}
	if err := checkHealth(client, "http://127.0.0.1:3600/health"); err != nil {
		log.Fatalf("health check failed: %v", err)
	}
}

func checkHealth(client *http.Client, endpoint string) error {
	response, err := client.Get(endpoint)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		if err := response.Body.Close(); err != nil {
			return fmt.Errorf("response returned %s and close failed: %w", response.Status, err)
		}
		return fmt.Errorf("health check returned %s", response.Status)
	}
	if err := response.Body.Close(); err != nil {
		return fmt.Errorf("response close failed: %w", err)
	}
	return nil
}

func processEnvironment() map[string]string {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, found := strings.Cut(entry, "=")
		if found {
			env[key] = value
		}
	}
	return env
}
