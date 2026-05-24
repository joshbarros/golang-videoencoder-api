package upload

import (
	"context"
	"io"
	"log"
	"os"
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
	Errors       []string
}

// NewVideoUpload creates an empty upload manager instance.
func NewVideoUpload() *VideoUpload {
	return &VideoUpload{}
}

// UploadObject uploads one local file into the configured output bucket.
func (vu *VideoUpload) UploadObject(objectPath string, client *storage.Client, ctx context.Context) error {
	path := strings.Split(objectPath, os.Getenv("localStoragePath")+"/")

	f, err := os.Open(objectPath)
	if err != nil {
		return err
	}

	defer f.Close()

	wc := client.Bucket(vu.OutputBucket).Object(path[1]).NewWriter(ctx)

	if _, err = io.Copy(wc, f); err != nil {
		return err
	}

	if err := wc.Close(); err != nil {
		return err
	}

	return nil
}

// loadPaths walks the encoded output directory and collects uploadable files.
func (vu *VideoUpload) loadPaths() error {
	err := filepath.Walk(vu.VideoPath, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			vu.Paths = append(vu.Paths, path)
		}

		return nil
	})

	if err != nil {
		return err
	}

	return nil
}

// ProcessUpload starts worker goroutines to upload discovered files concurrently.
func (vu *VideoUpload) ProcessUpload(concurrency int, doneUpload chan string) error {
	vu.Paths = nil
	vu.Errors = nil

	in := make(chan int, runtime.NumCPU())

	err := vu.loadPaths()
	if err != nil {
		return err
	}

	if len(vu.Paths) == 0 {
		doneUpload <- "upload complete"
		return nil
	}

	uploadClient, ctx, err := getClientUpload()
	if err != nil {
		return err
	}

	workerCount := concurrency
	if workerCount <= 0 {
		workerCount = 1
	}
	if workerCount > len(vu.Paths) {
		workerCount = len(vu.Paths)
	}

	var wg sync.WaitGroup
	var errorsMu sync.Mutex

	for process := 0; process < workerCount; process++ {
		wg.Add(1)
		go vu.uploadWorker(in, uploadClient, ctx, &wg, &errorsMu)
	}

	go func() {
		for x := 0; x < len(vu.Paths); x++ {
			in <- x
		}
		close(in)
	}()

	wg.Wait()

	if len(vu.Errors) > 0 {
		doneUpload <- vu.Errors[0]
		return nil
	}

	doneUpload <- "upload complete"

	return nil
}

// uploadWorker consumes file indexes and uploads each file through the shared client.
func (vu *VideoUpload) uploadWorker(in chan int, uploadClient *storage.Client, ctx context.Context, wg *sync.WaitGroup, errorsMu *sync.Mutex) {
	defer wg.Done()

	for x := range in {
		err := vu.UploadObject(vu.Paths[x], uploadClient, ctx)

		if err != nil {
			errorsMu.Lock()
			vu.Errors = append(vu.Errors, vu.Paths[x])
			errorsMu.Unlock()
			log.Printf("error during the upload: %v. Error: %v", vu.Paths[x], err)
		}
	}
}

// getClientUpload creates a GCS client and context for upload operations.
func getClientUpload() (*storage.Client, context.Context, error) {
	ctx := context.Background()

	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, nil, err
	}

	return client, ctx, nil
}
