package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/server"
	"github.com/arpitbhayani/pagebox/internal/storage"
	"github.com/arpitbhayani/pagebox/internal/ui"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the server and sync loop in the foreground",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := pbconfig.Load()
		if err != nil {
			ui.PrintErr("failed to load config", err.Error(), nil)
			os.Exit(1)
		}

		store, err := storage.New(cfg)
		if err != nil {
			ui.PrintErr("failed to init storage", err.Error(), nil)
			os.Exit(1)
		}

		idx, err := index.NewStore()
		if err != nil {
			ui.PrintErr("failed to open index", err.Error(), nil)
			os.Exit(1)
		}

		// Setup logging for daemon
		home, _ := os.UserHomeDir()
		logDir := filepath.Join(home, ".pagebox", "logs")
		os.MkdirAll(logDir, 0755)
		logFile, err := os.OpenFile(filepath.Join(logDir, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err == nil {
			if ui.IsTTY(os.Stderr) {
				log.SetOutput(io.MultiWriter(os.Stderr, logFile))
			} else {
				log.SetOutput(logFile)
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		syncLoop, err := server.NewSyncLoop(cfg, idx, store)
		if err != nil {
			ui.PrintErr("failed to init sync loop", err.Error(), nil)
			os.Exit(1)
		}

		go func() {
			if err := syncLoop.Start(ctx); err != nil {
				log.Printf("sync loop error: %v", err)
			}
		}()

		srv := server.New(cfg, idx, store)
		
		addr := fmt.Sprintf("%s:%d", cfg.Bind, cfg.Port)
		ln, err := net.Listen("tcp", addr)
		if err != nil && cfg.Port == 80 && strings.Contains(err.Error(), "permission denied") {
			// Unprivileged user fallback
			fallbackPort := 8080
			fallbackAddr := fmt.Sprintf("%s:%d", cfg.Bind, fallbackPort)
			ln2, err2 := net.Listen("tcp", fallbackAddr)
			if err2 == nil {
				ln = ln2
				addr = fallbackAddr
				cfg.Port = fallbackPort
				ui.PrintWarn("port 80 requires elevated privileges", fmt.Sprintf("falling back to http://%s", addr), nil)
				ui.Hint("to serve directly on port 80, run 'pagebox daemon install' to grant access", nil)
			} else {
				ui.PrintErr("failed to bind port 80", err.Error(), nil)
				ui.Hint("unprivileged users cannot bind to port 80. Run 'pagebox daemon install' to grant access.", nil)
				os.Exit(1)
			}
		} else if err != nil {
			ui.PrintErr(fmt.Sprintf("failed to bind to %s", addr), err.Error(), nil)
			os.Exit(1)
		}

		httpServer := &http.Server{
			Addr:    addr,
			Handler: srv,
		}

		ui.Heading("pagebox serve", nil, os.Stdout)
		ui.KV("documents", cfg.DocsDir, 12, os.Stdout)
		ui.KV("address", fmt.Sprintf("http://%s", addr), 12, os.Stdout)
		if cfg.Host != "" && cfg.Host != "127.0.0.1" && cfg.Host != "localhost" {
			ui.KV("host", fmt.Sprintf("http://%s", cfg.Host), 12, os.Stdout)
		}
		ui.Rule(os.Stdout)
		ui.PrintOk(fmt.Sprintf("serving on http://%s", addr), "", nil)
		ui.PrintInfo("watching folder", cfg.DocsDir, nil)
		ui.PrintInfo("running in foreground", "Press Ctrl+C to stop", nil)

		go func() {
			log.Printf("server listening on http://%s", addr)
			if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
				log.Fatalf("server error: %v", err)
			}
		}()

		// Graceful shutdown
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c

		ui.PrintInfo("shutting down gracefully...", "finishing in-flight pushes", nil)
		cancel() // stops sync loop polling

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("server shutdown error: %v", err)
		}
		
		ui.PrintOk("shutdown complete", "", nil)
	},
}


