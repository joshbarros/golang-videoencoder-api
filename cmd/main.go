package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"golang-videoencoder-api/internal/api"
	"golang-videoencoder-api/internal/database"
	"golang-videoencoder-api/internal/job"
	"golang-videoencoder-api/internal/queue"
	jobrepo "golang-videoencoder-api/internal/repositories/job"
	videorepo "golang-videoencoder-api/internal/repositories/video"

	_ "golang-videoencoder-api/docs" // generated Swagger spec

	"github.com/joho/godotenv"
)

var db database.Database

func init() {
	_ = godotenv.Load()

	automigrateDB, _ := strconv.ParseBool(os.Getenv("AUTO_MIGRATE_DB"))
	debug, _ := strconv.ParseBool(os.Getenv("DEBUG"))

	db.AutoMigrateDb = automigrateDB
	db.Debug = debug
	db.Dsn = os.Getenv("DSN")
	db.Env = os.Getenv("ENV")
}

// @title			Golang Video Encoder API
// @version		1.0
// @description	HTTP control plane for the queue-based video encoding pipeline: register videos, enqueue encode jobs, and track status. Processing happens asynchronously on the worker.
// @BasePath		/
func main() {
	log.Println("video encoder bootstrap starting")

	// Validate required configuration up front, before opening any resource,
	// so a misconfiguration fails fast without leaking connections.
	consumerName := requiredEnv("RABBITMQ_CONSUMER")
	if os.Getenv("DSN") == "" {
		log.Fatal("required environment variable is missing: DSN")
	}

	conn, err := db.Connect()
	if err != nil {
		log.Fatalf("database connection error: %v", err)
	}

	sqlDB, err := conn.DB()
	if err != nil {
		log.Fatalf("database handle error: %v", err)
	}
	defer sqlDB.Close()

	jobRepository := jobrepo.JobRepositoryDb{Db: conn}
	videoRepository := videorepo.VideoRepositoryDb{Db: conn}
	jobService := job.NewJobService(jobRepository, videoRepository)
	manager := job.NewManager(jobService)

	// Consumer connection (declares the queue topology).
	q, err := queue.NewClientFromEnv()
	if err != nil {
		log.Fatalf("queue configuration error: %v", err)
	}
	q.Prefetch = manager.Workers
	if err := q.Connect(); err != nil {
		log.Fatalf("rabbitmq connection error: %v", err)
	}
	defer func() {
		if err := q.Close(); err != nil {
			log.Printf("rabbitmq close warning: %v", err)
		}
	}()

	declaredQueue, err := q.Declare()
	if err != nil {
		log.Fatalf("queue declare error: %v", err)
	}

	// Separate publisher connection for the HTTP API, so reconnects on the
	// consumer side never race the API's publishes on a shared channel.
	pub, err := queue.NewClientFromEnv()
	if err != nil {
		log.Fatalf("publisher configuration error: %v", err)
	}
	if err := pub.Connect(); err != nil {
		log.Fatalf("publisher connection error: %v", err)
	}
	defer func() {
		if err := pub.Close(); err != nil {
			log.Printf("publisher close warning: %v", err)
		}
	}()

	stopWorkers := manager.Start()
	defer stopWorkers()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Consumer goroutine; consumerDone closes when it fully returns.
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		if err := manager.RunQueueConsumer(ctx, q, consumerName); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("consumer stopped: %v", err)
		}
	}()

	// HTTP API + Swagger.
	ready := func(ctx context.Context) error {
		if err := sqlDB.PingContext(ctx); err != nil {
			return errors.New("database unavailable")
		}
		if !pub.IsReady() {
			return errors.New("queue unavailable")
		}
		return nil
	}
	apiServer := api.NewServer(videoRepository, jobService, pub, ready)
	httpServer := &http.Server{
		Addr:              httpAddr(),
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	httpErrCh := make(chan error, 1)
	go func() { httpErrCh <- httpServer.ListenAndServe() }()

	if manager.Processor.Enabled() {
		log.Printf("media processing enabled: source=%s output=%s", manager.Processor.SourceBucket, manager.Processor.OutputBucket)
	} else {
		log.Println("media processing disabled (buckets unset): creating pending jobs only")
	}
	log.Printf("video encoder running: queue=%s workers=%d consumer=%s http=%s", declaredQueue.Name, manager.Workers, consumerName, httpServer.Addr)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("shutdown signal received: %s", sig.String())
	case <-consumerDone:
		log.Println("consumer exited; shutting down")
	case err := <-httpErrCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server stopped: %v", err)
		}
	}

	// Graceful shutdown: stop accepting HTTP, stop the consumer, then let the
	// deferred stopWorkers drain in-flight jobs.
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown warning: %v", err)
	}
	<-consumerDone

	log.Println("video encoder shutdown complete")
}

func requiredEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatalf("required environment variable is missing: %s", key)
	}

	return value
}

// httpAddr returns the HTTP listen address from env, defaulting to :8080.
func httpAddr() string {
	if addr := os.Getenv("HTTP_ADDR"); addr != "" {
		return addr
	}
	return ":8080"
}
