package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"golang-videoencoder-api/internal/database"
	"golang-videoencoder-api/internal/job"
	"golang-videoencoder-api/internal/queue"
	jobrepo "golang-videoencoder-api/internal/repositories/job"
	videorepo "golang-videoencoder-api/internal/repositories/video"

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

	stopWorkers := manager.Start()
	defer stopWorkers()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- manager.RunQueueConsumer(ctx, q, consumerName)
	}()

	if manager.Processor.Enabled() {
		log.Printf("media processing enabled: source=%s output=%s", manager.Processor.SourceBucket, manager.Processor.OutputBucket)
	} else {
		log.Println("media processing disabled (buckets unset): creating pending jobs only")
	}
	log.Printf("video encoder running: queue=%s workers=%d consumer=%s", declaredQueue.Name, manager.Workers, consumerName)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("shutdown signal received: %s", sig.String())
		cancel()
		if err := <-errCh; err != nil && err != context.Canceled {
			log.Printf("consumer shutdown warning: %v", err)
		}
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			log.Fatalf("consumer stopped with error: %v", err)
		}
	}

	log.Println("video encoder shutdown complete")
}

func requiredEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatalf("required environment variable is missing: %s", key)
	}

	return value
}
