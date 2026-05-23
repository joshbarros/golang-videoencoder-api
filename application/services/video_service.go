package services

import (
	"context"
	"golang-videoencoder-api/application/repositories"
	"golang-videoencoder-api/domain"
	"log"
	"os"
	"os/exec"

	"cloud.google.com/go/storage"
)

type VideoService struct {
	Video           *domain.Video
	VideoRepository repositories.VideoRepository
	storageClient   *storage.Client
}

func NewVideoService() (*VideoService, error) {
	storageClient, err := storage.NewClient(context.Background())
	if err != nil {
		return nil, err
	}

	return &VideoService{storageClient: storageClient}, nil
}

func (v *VideoService) Download(bucketName string) error {
	ctx := context.Background()
	reader, err := v.storageClient.Bucket(bucketName).Object(v.Video.FilePath).NewReader(ctx)
	if err != nil {
		return err
	}
	defer reader.Close()

	localFilePath := os.Getenv("localStoragePath") + "/" + v.Video.ID + ".mp4"
	f, err := os.Create(localFilePath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.ReadFrom(reader)
	if err != nil {
		return err
	}

	log.Printf("video %v has been stored at %s", v.Video.ID, localFilePath)
	return nil
}

func (v *VideoService) Fragment() error {
	err := os.Mkdir(os.Getenv("localStoragePath")+"/"+v.Video.ID, os.ModePerm)
	if err != nil {
		return err
	}

	source := os.Getenv("localStoragePath") + "/" + v.Video.ID + ".mp4"
	target := os.Getenv("localStoragePath") + "/" + v.Video.ID + ".frag"

	cmd := exec.Command("mp4fragment", source, target)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	printOutput(output)

	return nil
}

func (v *VideoService) Encode() error {
	cmdArgs := []string{}
	cmdArgs = append(cmdArgs, os.Getenv("localStoragePath")+"/"+v.Video.ID+".frag")
	cmdArgs = append(cmdArgs, "--use-segment-timeline")
	cmdArgs = append(cmdArgs, "-o")
	cmdArgs = append(cmdArgs, os.Getenv("localStoragePath")+"/"+v.Video.ID)
	cmdArgs = append(cmdArgs, "-f")
	cmdArgs = append(cmdArgs, "--exec-dir")
	cmdArgs = append(cmdArgs, "/opt/bento4/bin/")
	cmd := exec.Command("mp4dash", cmdArgs...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return err
	}

	printOutput(output)

	return nil
}

func (v *VideoService) Finish() error {
	err := os.Remove(os.Getenv("localStoragePath") + "/" + v.Video.ID + ".mp4")
	if err != nil {
		log.Println("error removing mp4 ", v.Video.ID, ".mp4")
		return err
	}

	err = os.Remove(os.Getenv("localStoragePath") + "/" + v.Video.ID + ".frag")
	if err != nil {
		log.Println("error removing frag ", v.Video.ID, ".frag")
		return err
	}

	err = os.RemoveAll(os.Getenv("localStoragePath") + "/" + v.Video.ID)
	if err != nil {
		log.Println("error removing mp4 ", v.Video.ID, ".mp4")
		return err
	}

	log.Println("files have been removed: ", v.Video.ID)

	return nil
}

func printOutput(out []byte) {
	if len(out) > 0 {
		log.Printf("========> Output: %s\n", string(out))
	}
}
