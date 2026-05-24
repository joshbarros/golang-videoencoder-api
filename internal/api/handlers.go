package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"golang-videoencoder-api/domain"
	"golang-videoencoder-api/internal/job"
	jobrepo "golang-videoencoder-api/internal/repositories/job"
	videorepo "golang-videoencoder-api/internal/repositories/video"

	uuid "github.com/satori/go.uuid"
)

// validUUID reports whether s is a well-formed UUID. IDs are stored in uuid
// columns, so a malformed id is a client error (400) rather than a lookup that
// would surface as a database error.
func validUUID(s string) bool {
	_, err := uuid.FromString(s)
	return err == nil
}

// handleHealthz godoc
//
//	@Summary		Liveness probe
//	@Description	Returns ok while the process is running.
//	@Tags			health
//	@Produce		json
//	@Success		200	{object}	HealthResponse
//	@Router			/healthz [get]
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok"})
}

// handleReadyz godoc
//
//	@Summary		Readiness probe
//	@Description	Returns ok when the database and queue are reachable.
//	@Tags			health
//	@Produce		json
//	@Success		200	{object}	HealthResponse
//	@Failure		503	{object}	ErrorResponse
//	@Router			/readyz [get]
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if s.ready != nil {
		if err := s.ready(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok"})
}

// handleCreateVideo godoc
//
//	@Summary		Register a source video
//	@Description	Stores a source media reference that jobs can then encode.
//	@Tags			videos
//	@Accept			json
//	@Produce		json
//	@Param			video	body		CreateVideoRequest	true	"Video to register"
//	@Success		201		{object}	VideoResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Router			/videos [post]
func (s *Server) handleCreateVideo(w http.ResponseWriter, r *http.Request) {
	var req CreateVideoRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	req.ResourceID = strings.TrimSpace(req.ResourceID)
	req.FilePath = strings.TrimSpace(req.FilePath)
	if req.ResourceID == "" || req.FilePath == "" {
		writeError(w, http.StatusBadRequest, "resource_id and file_path are required")
		return
	}

	video := &domain.Video{ResourceID: req.ResourceID, FilePath: req.FilePath}
	created, err := s.videos.Insert(video)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, toVideoResponse(created))
}

// handleGetVideo godoc
//
//	@Summary		Get a video and its jobs
//	@Tags			videos
//	@Produce		json
//	@Param			id	path		string	true	"Video ID (UUID)"
//	@Success		200	{object}	VideoResponse
//	@Failure		400	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/videos/{id} [get]
func (s *Server) handleGetVideo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validUUID(id) {
		writeError(w, http.StatusBadRequest, "invalid video id: must be a UUID")
		return
	}

	video, err := s.videos.Find(id)
	if err != nil {
		if errors.Is(err, videorepo.ErrVideoNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, toVideoResponse(video))
}

// handleCreateJob godoc
//
//	@Summary		Enqueue an encode job
//	@Description	Publishes a job for the referenced video onto the work queue. Processing is asynchronous; poll the job or video to track status.
//	@Tags			jobs
//	@Accept			json
//	@Produce		json
//	@Param			job	body		CreateJobRequest	true	"Job to enqueue"
//	@Success		202	{object}	EnqueueResponse
//	@Failure		400	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		502	{object}	ErrorResponse
//	@Router			/jobs [post]
func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req CreateJobRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	req.VideoID = strings.TrimSpace(req.VideoID)
	req.OutputBucketPath = strings.TrimSpace(req.OutputBucketPath)
	if req.VideoID == "" || req.OutputBucketPath == "" {
		writeError(w, http.StatusBadRequest, "video_id and output_bucket_path are required")
		return
	}
	if !validUUID(req.VideoID) {
		writeError(w, http.StatusBadRequest, "video_id must be a UUID")
		return
	}

	// Reject early if the referenced video does not exist, instead of letting
	// the message fail later and dead-letter.
	if _, err := s.videos.Find(req.VideoID); err != nil {
		if errors.Is(err, videorepo.ErrVideoNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	msg := job.JobWorkerMessage{VideoID: req.VideoID, OutputBucketPath: req.OutputBucketPath}
	if err := s.publisher.PublishJSON(r.Context(), msg); err != nil {
		writeError(w, http.StatusBadGateway, "failed to enqueue job: "+err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, EnqueueResponse{
		VideoID:          req.VideoID,
		OutputBucketPath: req.OutputBucketPath,
		Status:           "queued",
	})
}

// handleGetJob godoc
//
//	@Summary		Get a job's status
//	@Tags			jobs
//	@Produce		json
//	@Param			id	path		string	true	"Job ID (UUID)"
//	@Success		200	{object}	JobResponse
//	@Failure		400	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/jobs/{id} [get]
func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validUUID(id) {
		writeError(w, http.StatusBadRequest, "invalid job id: must be a UUID")
		return
	}

	j, err := s.jobs.Find(id)
	if err != nil {
		if errors.Is(err, jobrepo.ErrJobNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, toJobResponse(j))
}

// decodeJSON strictly decodes a JSON request body.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
