package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
			log.SetOutput(logFile)
			// also print to stderr if run in foreground attached to tty
			// actually, standard log only goes to one writer.
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
		httpServer := &http.Server{
			Addr:    addr,
			Handler: srv,
		}

		go func() {
			log.Printf("server listening on http://%s", addr)
			ui.PrintOk(fmt.Sprintf("serving on http://%s", addr), "", nil)
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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


