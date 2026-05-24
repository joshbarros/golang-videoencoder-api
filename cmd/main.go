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
	db.DbType = os.Getenv("DB_TYPE")
	db.Dsn = os.Getenv("DSN")
	db.Env = os.Getenv("ENV")
}

func main() {
	log.Println("video encoder bootstrap starting")

	conn, err := db.Connect()
	if err != nil {
		log.Fatalf("database connection error: %v", err)
	}

	sqlDB, err := conn.DB()
	if err != nil {
		log.Fatalf("database handle error: %v", err)
	}
	defer sqlDB.Close()

	q, err := queue.NewClientFromEnv()
	if err != nil {
		log.Fatalf("queue configuration error: %v", err)
	}
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

	jobRepository := jobrepo.JobRepositoryDb{Db: conn}
	videoRepository := videorepo.VideoRepositoryDb{Db: conn}
	jobService := job.NewJobService(jobRepository, videoRepository)
	manager := job.NewManager(jobService)
	stopWorkers := manager.Start()
	defer stopWorkers()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	consumerName := requiredEnv("RABBITMQ_CONSUMER")

	errCh := make(chan error, 1)
	go func() {
		errCh <- manager.RunQueueConsumer(ctx, q, consumerName)
	}()

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
