// Package api provides the HTTP control plane for the video encoder: it
// registers source videos, enqueues encode jobs onto the work queue, and
// exposes job/video status plus health and readiness probes.
package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"golang-videoencoder-api/domain"
	"golang-videoencoder-api/internal/job"
	videorepo "golang-videoencoder-api/internal/repositories/video"

	httpSwagger "github.com/swaggo/http-swagger/v2"
)

// Publisher enqueues a job payload onto the work queue.
type Publisher interface {
	PublishJSON(ctx context.Context, payload any) error
}

// Server holds the dependencies for the HTTP handlers.
type Server struct {
	videos    videorepo.VideoRepository
	jobs      *job.JobService
	publisher Publisher
	// ready performs a readiness check (DB, queue). nil means always ready.
	ready func(context.Context) error
}

// NewServer builds an HTTP API server.
func NewServer(videos videorepo.VideoRepository, jobs *job.JobService, publisher Publisher, ready func(context.Context) error) *Server {
	return &Server{videos: videos, jobs: jobs, publisher: publisher, ready: ready}
}

// Handler returns the configured HTTP router, including the Swagger UI.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("POST /videos", s.handleCreateVideo)
	mux.HandleFunc("GET /videos/{id}", s.handleGetVideo)
	mux.HandleFunc("POST /jobs", s.handleCreateJob)
	mux.HandleFunc("GET /jobs/{id}", s.handleGetJob)

	// Swagger UI at /swagger/index.html, spec served from the generated docs.
	mux.Handle("GET /swagger/", httpSwagger.WrapHandler)

	return logRequests(mux)
}

// ----- request/response DTOs -----

// CreateVideoRequest is the body for registering a source video.
type CreateVideoRequest struct {
	ResourceID string `json:"resource_id" example:"campaign-42"`
	FilePath   string `json:"file_path" example:"invite.mp4"`
}

// VideoResponse describes a registered video and its jobs.
type VideoResponse struct {
	ID         string        `json:"id"`
	ResourceID string        `json:"resource_id"`
	FilePath   string        `json:"file_path"`
	CreatedAt  time.Time     `json:"created_at"`
	Jobs       []JobResponse `json:"jobs,omitempty"`
}

// CreateJobRequest is the body for enqueuing an encode job.
type CreateJobRequest struct {
	VideoID          string `json:"video_id" example:"7d9b0e0e-2c1a-4b3c-8a1f-1a2b3c4d5e6f"`
	OutputBucketPath string `json:"output_bucket_path" example:"out/encoded/7d9b.../"`
}

// EnqueueResponse acknowledges that a job was queued for processing.
type EnqueueResponse struct {
	VideoID          string `json:"video_id"`
	OutputBucketPath string `json:"output_bucket_path"`
	Status           string `json:"status" example:"queued"`
}

// JobResponse describes a job and its current status.
type JobResponse struct {
	ID               string    `json:"id"`
	VideoID          string    `json:"video_id"`
	Status           string    `json:"status" example:"Completed"`
	OutputBucketPath string    `json:"output_bucket_path"`
	Error            string    `json:"error,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// HealthResponse is returned by the liveness/readiness probes.
type HealthResponse struct {
	Status string `json:"status" example:"ok"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error" example:"video does not exist"`
}

// ----- mappers -----

func toVideoResponse(v *domain.Video) VideoResponse {
	resp := VideoResponse{
		ID:         v.ID,
		ResourceID: v.ResourceID,
		FilePath:   v.FilePath,
		CreatedAt:  v.CreatedAt,
	}
	for _, j := range v.Jobs {
		resp.Jobs = append(resp.Jobs, toJobResponse(j))
	}
	return resp
}

func toJobResponse(j *domain.Job) JobResponse {
	return JobResponse{
		ID:               j.ID,
		VideoID:          j.VideoID,
		Status:           j.Status,
		OutputBucketPath: j.OutputBucketPath,
		Error:            j.Error,
		CreatedAt:        j.CreatedAt,
		UpdatedAt:        j.UpdatedAt,
	}
}

// ----- helpers -----

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload != nil {
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			log.Printf("api: encode response: %v", err)
		}
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

// writeInternalError logs the underlying error server-side and returns a
// generic message, so internal details (e.g. raw database errors) never leak
// to clients.
func writeInternalError(w http.ResponseWriter, context string, err error) {
	log.Printf("api: %s: %v", context, err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

// logRequests logs method, path, status, and duration for each request.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("api: %s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
