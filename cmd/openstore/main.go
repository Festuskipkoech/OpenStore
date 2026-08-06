package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"openstore/internal/config"
	"openstore/internal/db"
	"openstore/internal/handlers"
	"openstore/internal/middleware"
	"openstore/internal/seaweedfs"
	"openstore/internal/security"
	"openstore/internal/webhook"
	"openstore/internal/jobs"
)

func main() {
	_ = godotenv.Load()

	setupLogger("info")


	cfg, err := config.Load()
	if err != nil {
		slog.Error("config error", "error", err)
		os.Exit(1)
	}

	setupLogger(cfg.LogLevel)

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		slog.Error("database error", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	slog.Info("database ready", "path", cfg.DBPath)

	seaweedClient, err := seaweedfs.New(cfg.SeaweedFSFilerAddr, cfg.SeaweedFSFilerHTTPAddr)
	if err != nil {
		slog.Error("seaweedfs connection error", "error", err)
		os.Exit(1)
	}
	defer seaweedClient.Close()

	slog.Info("seaweedfs filer connected", "addr", cfg.SeaweedFSFilerAddr)

	tokenizer := security.NewTokenizer(cfg.APIKey)

	healthHandler := handlers.NewHealthHandler(database, seaweedClient)
	configureHandler := handlers.NewConfigureHandler(database)
	uploadHandler := handlers.NewUploadHandler(database, seaweedClient, tokenizer, cfg)
	filesHandler := handlers.NewFilesHandler(database, seaweedClient, tokenizer, cfg)

	r := chi.NewRouter()
	r.Use(chimiddleware.ClientIPFromHeader("X-Real-IP"))
	r.Use(middleware.Recovery)
	r.Use(middleware.Logger)

	r.Get("/health", healthHandler.Shallow)
	r.Get("/health/deep", healthHandler.Deep)

	// PUT /upload/{uploadID} is outside the API key auth group — browser authenticates
	// via HMAC token in the query string.
	r.Put("/upload/{uploadID}", uploadHandler.Stream)

	// GET /files/{uploadID} is outside the auth group — public buckets stream with no
	// credentials, private buckets authenticate via the signed read token in the query string.
	r.Get("/files/{uploadID}", filesHandler.ReadFile)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(cfg.APIKey))

		r.Post("/configure", configureHandler.Create)
		r.Get("/configure", configureHandler.Get)
		r.Put("/configure", configureHandler.Update)
		r.Patch("/configure/buckets/{bucketName}", configureHandler.PatchBucket)
		r.Delete("/configure", configureHandler.Delete)
		r.Delete("/configure/buckets/{bucketName}", configureHandler.DeleteBucket)

		r.Post("/upload/presign", uploadHandler.Presign)

		r.Get("/uploads/{uploadID}", filesHandler.GetUploadStatus)
		r.Post("/files/{uploadID}/read-presign", filesHandler.PresignRead)
		r.Delete("/files/{uploadID}", filesHandler.DeleteFile)
	})

	ctx, cancel := context.WithCancel(context.Background())

	go webhook.StartRetryWorker(ctx, database)
	go jobs.StartSweeper(ctx, database, seaweedClient)

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: r,
		ReadTimeout: 30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout: 120 * time.Second,
	}

	go func() {
		slog.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	
	cancel()
	slog.Info("shutting down")
	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}

func setupLogger(level string) {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l})))
}