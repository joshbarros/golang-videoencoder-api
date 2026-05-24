package video

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"

	"golang-videoencoder-api/domain"
	videorepo "golang-videoencoder-api/internal/repositories/video"

	"cloud.google.com/go/storage"
)

// VideoService orchestrates download, packaging, and cleanup for a video.
type VideoService struct {
	Video           *domain.Video
	VideoRepository videorepo.VideoRepository
	storageClient   *storage.Client
}

// NewVideoService builds a VideoService with an initialized GCS client.
func NewVideoService() (*VideoService, error) {
	storageClient, err := storage.NewClient(context.Background())
	if err != nil {
		return nil, err
	}

	return &VideoService{storageClient: storageClient}, nil
}

// Close releases the GCS client held by the service.
func (v *VideoService) Close() error {
	if v.storageClient != nil {
		return v.storageClient.Close()
	}

	return nil
}

// basePath returns the configured local working directory.
func basePath() string {
	return os.Getenv("localStoragePath")
}

// Download fetches the source media from the provided GCS bucket into local storage.
func (v *VideoService) Download(ctx context.Context, bucketName string) error {
	reader, err := v.storageClient.Bucket(bucketName).Object(v.Video.FilePath).NewReader(ctx)
	if err != nil {
		return err
	}
	defer reader.Close()

	localFilePath := basePath() + "/" + v.Video.ID + ".mp4"
	f, err := os.Create(localFilePath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err = f.ReadFrom(reader); err != nil {
		return err
	}

	log.Printf("video %v has been stored at %s", v.Video.ID, localFilePath)
	return nil
}

// Fragment generates a fragmented MP4 file from the downloaded media.
func (v *VideoService) Fragment(ctx context.Context) error {
	if err := os.Mkdir(basePath()+"/"+v.Video.ID, os.ModePerm); err != nil {
		return err
	}

	source := basePath() + "/" + v.Video.ID + ".mp4"
	target := basePath() + "/" + v.Video.ID + ".frag"

	cmd := exec.CommandContext(ctx, "mp4fragment", source, target)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	printOutput(output)
	return nil
}

// Encode creates DASH output files from the fragmented input using Bento4.
func (v *VideoService) Encode(ctx context.Context) error {
	cmdArgs := []string{
		basePath() + "/" + v.Video.ID + ".frag",
		"--use-segment-timeline",
		"-o", basePath() + "/" + v.Video.ID,
		"-f",
		"--exec-dir", "/opt/bento4/bin/",
	}

	cmd := exec.CommandContext(ctx, "mp4dash", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	printOutput(output)
	return nil
}

// Finish removes temporary files and generated local output for the current
// video. It is best-effort: every artifact is attempted even if an earlier
// removal fails, and an already-absent artifact is not treated as an error.
func (v *VideoService) Finish() error {
	targets := []string{
		basePath() + "/" + v.Video.ID + ".mp4",
		basePath() + "/" + v.Video.ID + ".frag",
		basePath() + "/" + v.Video.ID,
	}

	var errs []error
	for _, target := range targets {
		if err := os.RemoveAll(target); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", target, err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	log.Println("files have been removed: ", v.Video.ID)
	return nil
}

// printOutput logs command output when external tools return any text.
func printOutput(out []byte) {
	if len(out) > 0 {
		log.Printf("========> Output: %s\n", string(out))
	}
}
