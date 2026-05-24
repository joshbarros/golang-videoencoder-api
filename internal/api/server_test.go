package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang-videoencoder-api/internal/api"
	"golang-videoencoder-api/internal/database"
	"golang-videoencoder-api/internal/job"
	jobrepo "golang-videoencoder-api/internal/repositories/job"
	videorepo "golang-videoencoder-api/internal/repositories/video"

	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
)

type fakePublisher struct {
	published []any
	err       error
}

func (f *fakePublisher) PublishJSON(_ context.Context, payload any) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, payload)
	return nil
}

type testEnv struct {
	handler   http.Handler
	videos    videorepo.VideoRepositoryDb
	jobs      *job.JobService
	publisher *fakePublisher
}

func newTestEnv(t *testing.T, ready func(context.Context) error) testEnv {
	t.Helper()
	db := database.NewDbTest()
	videos := videorepo.VideoRepositoryDb{Db: db}
	jobs := job.NewJobService(jobrepo.JobRepositoryDb{Db: db}, videos)
	pub := &fakePublisher{}
	srv := api.NewServer(videos, jobs, pub, ready)
	return testEnv{handler: srv.Handler(), videos: videos, jobs: jobs, publisher: pub}
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthz(t *testing.T) {
	env := newTestEnv(t, nil)
	rec := do(t, env.handler, http.MethodGet, "/healthz", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestReadyz(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		env := newTestEnv(t, func(context.Context) error { return nil })
		rec := do(t, env.handler, http.MethodGet, "/readyz", nil)
		require.Equal(t, http.StatusOK, rec.Code)
	})
	t.Run("not ready", func(t *testing.T) {
		env := newTestEnv(t, func(context.Context) error { return errors.New("queue unavailable") })
		rec := do(t, env.handler, http.MethodGet, "/readyz", nil)
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Contains(t, rec.Body.String(), "queue unavailable")
	})
}

func TestCreateVideo(t *testing.T) {
	env := newTestEnv(t, nil)

	rec := do(t, env.handler, http.MethodPost, "/videos", api.CreateVideoRequest{ResourceID: "campaign-1", FilePath: "invite.mp4"})
	require.Equal(t, http.StatusCreated, rec.Code)

	var resp api.VideoResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.ID)
	require.Equal(t, "campaign-1", resp.ResourceID)
	require.Equal(t, "invite.mp4", resp.FilePath)
}

func TestCreateVideoValidation(t *testing.T) {
	env := newTestEnv(t, nil)

	rec := do(t, env.handler, http.MethodPost, "/videos", api.CreateVideoRequest{ResourceID: "", FilePath: ""})
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// malformed JSON
	req := httptest.NewRequest(http.MethodPost, "/videos", bytes.NewReader([]byte("{not json")))
	rec2 := httptest.NewRecorder()
	env.handler.ServeHTTP(rec2, req)
	require.Equal(t, http.StatusBadRequest, rec2.Code)
}

func TestCreateVideoOversizedField(t *testing.T) {
	env := newTestEnv(t, nil)
	// >255 chars must be rejected as 400, never reach the DB (no varchar overflow 500).
	rec := do(t, env.handler, http.MethodPost, "/videos",
		api.CreateVideoRequest{ResourceID: strings.Repeat("A", 300), FilePath: "invite.mp4"})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.NotContains(t, rec.Body.String(), "SQLSTATE", "must not leak DB errors")
}

func TestRequestBodyTooLarge(t *testing.T) {
	env := newTestEnv(t, nil)
	// ~2 MiB body exceeds the 1 MiB cap -> 413, decoded lazily without buffering it all.
	huge := `{"resource_id":"` + strings.Repeat("A", 2<<20) + `","file_path":"invite.mp4"}`
	req := httptest.NewRequest(http.MethodPost, "/videos", strings.NewReader(huge))
	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestCreateVideoRejectsNulByte(t *testing.T) {
	env := newTestEnv(t, nil)
	// A NUL byte would fail the Postgres text insert; must be rejected as 400.
	rec := do(t, env.handler, http.MethodPost, "/videos",
		api.CreateVideoRequest{ResourceID: "bad\x00id", FilePath: "invite.mp4"})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.NotContains(t, rec.Body.String(), "internal server error")
}

func TestRejectsTrailingJSON(t *testing.T) {
	env := newTestEnv(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/videos",
		strings.NewReader(`{"resource_id":"a","file_path":"b"}{"x":1}`))
	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetVideo(t *testing.T) {
	env := newTestEnv(t, nil)

	created := do(t, env.handler, http.MethodPost, "/videos", api.CreateVideoRequest{ResourceID: "r", FilePath: "invite.mp4"})
	var v api.VideoResponse
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &v))

	rec := do(t, env.handler, http.MethodGet, "/videos/"+v.ID, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	// valid UUID, no such video -> 404
	missing := do(t, env.handler, http.MethodGet, "/videos/"+uuid.NewV4().String(), nil)
	require.Equal(t, http.StatusNotFound, missing.Code)

	// malformed id -> 400 (not a DB error / 500)
	malformed := do(t, env.handler, http.MethodGet, "/videos/not-a-uuid", nil)
	require.Equal(t, http.StatusBadRequest, malformed.Code)
}

func TestCreateJob(t *testing.T) {
	env := newTestEnv(t, nil)

	// seed a video
	created := do(t, env.handler, http.MethodPost, "/videos", api.CreateVideoRequest{ResourceID: "r", FilePath: "invite.mp4"})
	var v api.VideoResponse
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &v))

	rec := do(t, env.handler, http.MethodPost, "/jobs", api.CreateJobRequest{VideoID: v.ID, OutputBucketPath: "out/x/"})
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Len(t, env.publisher.published, 1)

	msg, ok := env.publisher.published[0].(job.JobWorkerMessage)
	require.True(t, ok, "published payload should be a JobWorkerMessage")
	require.Equal(t, v.ID, msg.VideoID)
	require.Equal(t, "out/x/", msg.OutputBucketPath)
}

func TestCreateJobUnknownVideo(t *testing.T) {
	env := newTestEnv(t, nil)
	// valid UUID, no such video -> 404
	rec := do(t, env.handler, http.MethodPost, "/jobs", api.CreateJobRequest{VideoID: uuid.NewV4().String(), OutputBucketPath: "out/"})
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Empty(t, env.publisher.published, "must not enqueue for a missing video")

	// malformed video_id -> 400
	bad := do(t, env.handler, http.MethodPost, "/jobs", api.CreateJobRequest{VideoID: "nope", OutputBucketPath: "out/"})
	require.Equal(t, http.StatusBadRequest, bad.Code)
}

func TestCreateJobValidation(t *testing.T) {
	env := newTestEnv(t, nil)
	rec := do(t, env.handler, http.MethodPost, "/jobs", api.CreateJobRequest{VideoID: "", OutputBucketPath: ""})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateJobPublisherFailure(t *testing.T) {
	env := newTestEnv(t, nil)
	env.publisher.err = errors.New("broker down")

	created := do(t, env.handler, http.MethodPost, "/videos", api.CreateVideoRequest{ResourceID: "r", FilePath: "invite.mp4"})
	var v api.VideoResponse
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &v))

	rec := do(t, env.handler, http.MethodPost, "/jobs", api.CreateJobRequest{VideoID: v.ID, OutputBucketPath: "out/"})
	require.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestGetJob(t *testing.T) {
	env := newTestEnv(t, nil)

	// seed video + a pending job directly via the service
	created := do(t, env.handler, http.MethodPost, "/videos", api.CreateVideoRequest{ResourceID: "r", FilePath: "invite.mp4"})
	var v api.VideoResponse
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &v))

	j, err := env.jobs.CreatePendingJob(v.ID, "out/")
	require.NoError(t, err)

	rec := do(t, env.handler, http.MethodGet, "/jobs/"+j.ID, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp api.JobResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, j.ID, resp.ID)
	require.Equal(t, job.JobStatusPending, resp.Status)

	// valid UUID, no such job -> 404
	missing := do(t, env.handler, http.MethodGet, "/jobs/"+uuid.NewV4().String(), nil)
	require.Equal(t, http.StatusNotFound, missing.Code)

	// malformed id -> 400
	malformed := do(t, env.handler, http.MethodGet, "/jobs/not-a-uuid", nil)
	require.Equal(t, http.StatusBadRequest, malformed.Code)
}
