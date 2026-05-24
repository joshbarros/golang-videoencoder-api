package job

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"golang-videoencoder-api/domain"
	"golang-videoencoder-api/internal/database"
	jobrepo "golang-videoencoder-api/internal/repositories/job"
	videorepo "golang-videoencoder-api/internal/repositories/video"

	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
)

// fakeAck records how a message was acknowledged.
type fakeAck struct {
	acked   bool
	nacked  bool
	requeue bool
}

func (f *fakeAck) Ack() error              { f.acked = true; return nil }
func (f *fakeAck) Nack(requeue bool) error { f.nacked = true; f.requeue = requeue; return nil }

// newService builds a JobService backed by a fresh in-memory database, and
// returns the service plus the video repository for seeding test data.
func newService(t *testing.T) (*JobService, videorepo.VideoRepositoryDb) {
	t.Helper()
	db := database.NewDbTest()
	videoRepo := videorepo.VideoRepositoryDb{Db: db}
	jobRepo := jobrepo.JobRepositoryDb{Db: db}
	return NewJobService(jobRepo, videoRepo), videoRepo
}

func seedVideo(t *testing.T, repo videorepo.VideoRepositoryDb) *domain.Video {
	t.Helper()
	v := domain.NewVideo()
	v.ID = uuid.NewV4().String()
	v.FilePath = "invite.mp4"
	v.CreatedAt = time.Now()
	_, err := repo.Insert(v)
	require.NoError(t, err)
	return v
}

func TestParseJobWorkerMessage(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		wantErr bool
	}{
		{"valid", `{"video_id":"v1","output_bucket_path":"out/"}`, false},
		{"malformed json", `{`, true},
		{"missing video_id", `{"output_bucket_path":"out/"}`, true},
		{"missing output_bucket_path", `{"video_id":"v1"}`, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseJobWorkerMessage([]byte(c.payload))
			if c.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestProcessorEnabled(t *testing.T) {
	svc := NewJobService(nil, nil)

	var nilProcessor *Processor
	require.False(t, nilProcessor.Enabled(), "nil processor")
	require.False(t, (&Processor{}).Enabled(), "no job service or buckets")
	require.False(t, (&Processor{SourceBucket: "s", OutputBucket: "o"}).Enabled(), "no job service")
	require.False(t, (&Processor{JobService: svc, SourceBucket: "s"}).Enabled(), "no output bucket")
	require.True(t, (&Processor{JobService: svc, SourceBucket: "s", OutputBucket: "o"}).Enabled(), "fully configured")
}

func TestJobWorkerCreatesPendingJobAndAcks(t *testing.T) {
	svc, videoRepo := newService(t)
	v := seedVideo(t, videoRepo)

	msgCh := make(chan Message, 1)
	resCh := make(chan JobWorkerResult, 1)
	go JobWorker(msgCh, resCh, svc, nil) // nil processor => pending-only

	ack := &fakeAck{}
	msgCh <- Message{Body: []byte(fmt.Sprintf(`{"video_id":%q,"output_bucket_path":"out/"}`, v.ID)), Ack: ack}
	res := <-resCh
	close(msgCh)

	require.Empty(t, res.Error)
	require.NotNil(t, res.Job)
	require.Equal(t, JobStatusPending, res.Job.Status)
	require.Equal(t, v.ID, res.Job.VideoID)
	require.True(t, ack.acked, "successful message should be acked")
	require.False(t, ack.nacked)
}

func TestJobWorkerNacksMalformedMessage(t *testing.T) {
	msgCh := make(chan Message, 1)
	resCh := make(chan JobWorkerResult, 1)
	go JobWorker(msgCh, resCh, NewJobService(nil, nil), nil)

	ack := &fakeAck{}
	msgCh <- Message{Body: []byte(`not-json`), Ack: ack}
	res := <-resCh
	close(msgCh)

	require.NotEmpty(t, res.Error)
	require.Nil(t, res.Job)
	require.True(t, ack.nacked, "bad message should be dead-lettered")
	require.False(t, ack.requeue, "should not requeue a poison message")
	require.False(t, ack.acked)
}

func TestJobWorkerNacksWhenVideoMissing(t *testing.T) {
	svc, _ := newService(t) // no video seeded

	msgCh := make(chan Message, 1)
	resCh := make(chan JobWorkerResult, 1)
	go JobWorker(msgCh, resCh, svc, nil)

	ack := &fakeAck{}
	missingID := uuid.NewV4().String()
	msgCh <- Message{Body: []byte(fmt.Sprintf(`{"video_id":%q,"output_bucket_path":"out/"}`, missingID)), Ack: ack}
	res := <-resCh
	close(msgCh)

	require.Contains(t, res.Error, "video does not exist")
	require.True(t, ack.nacked)
	require.False(t, ack.acked)
}

func TestJobServiceStatusTransitions(t *testing.T) {
	svc, videoRepo := newService(t)
	v := seedVideo(t, videoRepo)

	job, err := svc.CreatePendingJob(v.ID, "out/")
	require.NoError(t, err)
	require.Equal(t, JobStatusPending, job.Status)

	processing, err := svc.MarkProcessing(job.ID)
	require.NoError(t, err)
	require.Equal(t, JobStatusProcessing, processing.Status)

	completed, err := svc.MarkCompleted(job.ID)
	require.NoError(t, err)
	require.Equal(t, JobStatusCompleted, completed.Status)
	require.Empty(t, completed.Error)

	failed, err := svc.MarkFailed(job.ID, errors.New("boom"))
	require.NoError(t, err)
	require.Equal(t, JobStatusFailed, failed.Status)
	require.Equal(t, "boom", failed.Error)
}

func TestManagerStartProcessesEnqueuedMessage(t *testing.T) {
	svc, videoRepo := newService(t)
	v := seedVideo(t, videoRepo)

	m := &Manager{
		JobService:     svc,
		Workers:        2,
		Processor:      &Processor{JobService: svc}, // disabled: no buckets => pending-only
		MessageChannel: make(chan Message, 2),
		ResultChannel:  make(chan JobWorkerResult),
	}

	stop := m.Start()
	m.Enqueue([]byte(fmt.Sprintf(`{"video_id":%q,"output_bucket_path":"out/"}`, v.ID)))
	stop() // closes input, waits for workers to drain, then closes output

	// The enqueued message is processed before stop() returns; a pending job
	// should now exist for the seeded video.
	loaded, err := videoRepo.Find(v.ID)
	require.NoError(t, err)
	require.Len(t, loaded.Jobs, 1)
	require.Equal(t, JobStatusPending, loaded.Jobs[0].Status)
}
