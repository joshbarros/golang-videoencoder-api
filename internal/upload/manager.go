package upload

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"cloud.google.com/go/storage"
)

// VideoUpload coordinates local output discovery and uploads to GCS.
type VideoUpload struct {
	Paths        []string
	VideoPath    string
	OutputBucket string
	// OutputPrefix is prepended to each uploaded object's name (e.g. the job's
	// output bucket path). When empty, the encode directory name is used so
	// outputs from different videos do not collide.
	OutputPrefix string
	Errors       []string
}

// NewVideoUpload creates an empty upload manager instance.
func NewVideoUpload() *VideoUpload {
	return &VideoUpload{}
}

// objectName derives the destination object name for a local file: its path
// relative to VideoPath, prefixed with OutputPrefix.
func (vu *VideoUpload) objectName(localPath string) string {
	rel := strings.TrimPrefix(localPath, vu.VideoPath+"/")

	prefix := vu.OutputPrefix
	if prefix == "" {
		prefix = filepath.Base(vu.VideoPath)
	}

	return path.Join(prefix, rel)
}

// UploadObject uploads one local file into the configured output bucket.
func (vu *VideoUpload) UploadObject(ctx context.Context, objectPath string, client *storage.Client) error {
	f, err := os.Open(objectPath)
	if err != nil {
		return err
	}
	defer f.Close()

	wc := client.Bucket(vu.OutputBucket).Object(vu.objectName(objectPath)).NewWriter(ctx)
	if _, err = io.Copy(wc, f); err != nil {
		_ = wc.Close()
		return err
	}

	return wc.Close()
}

// loadPaths walks the encoded output directory and collects uploadable files.
func (vu *VideoUpload) loadPaths() error {
	return filepath.Walk(vu.VideoPath, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			vu.Paths = append(vu.Paths, p)
		}
		return nil
	})
}

// ProcessUpload uploads every file under VideoPath concurrently. It returns nil
// only when all uploads succeed, and an error otherwise.
func (vu *VideoUpload) ProcessUpload(ctx context.Context, concurrency int) error {
	vu.Paths = nil
	vu.Errors = nil

	if err := vu.loadPaths(); err != nil {
		return fmt.Errorf("load output paths: %w", err)
	}

	if len(vu.Paths) == 0 {
		return fmt.Errorf("no output files found under %s", vu.VideoPath)
	}

	uploadClient, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("storage client: %w", err)
	}
	defer uploadClient.Close()

	workerCount := concurrency
	if workerCount <= 0 {
		workerCount = 1
	}
	if workerCount > len(vu.Paths) {
		workerCount = len(vu.Paths)
	}

	in := make(chan int, runtime.NumCPU())
	var wg sync.WaitGroup
	var errorsMu sync.Mutex

	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go vu.uploadWorker(ctx, in, uploadClient, &wg, &errorsMu)
	}

	for x := 0; x < len(vu.Paths); x++ {
		in <- x
	}
	close(in)

	wg.Wait()

	if len(vu.Errors) > 0 {
		return fmt.Errorf("%d of %d uploads failed (e.g. %s)", len(vu.Errors), len(vu.Paths), vu.Errors[0])
	}

	return nil
}

// uploadWorker consumes file indexes and uploads each file through the shared client.
func (vu *VideoUpload) uploadWorker(ctx context.Context, in chan int, uploadClient *storage.Client, wg *sync.WaitGroup, errorsMu *sync.Mutex) {
	defer wg.Done()

	for x := range in {
		if err := vu.UploadObject(ctx, vu.Paths[x], uploadClient); err != nil {
			errorsMu.Lock()
			vu.Errors = append(vu.Errors, vu.Paths[x])
			errorsMu.Unlock()
			log.Printf("error during the upload of %s: %v", vu.Paths[x], err)
		}
	}
}
