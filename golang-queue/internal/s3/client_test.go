package s3

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

type uploadCall struct {
	key         string
	contentType string
}

type fakeUploader struct {
	calls  []uploadCall
	failAt int
}

func (f *fakeUploader) Upload(_ context.Context, input *awss3.PutObjectInput, _ ...func(*manager.Uploader)) (*manager.UploadOutput, error) {
	f.calls = append(f.calls, uploadCall{
		key:         aws.ToString(input.Key),
		contentType: aws.ToString(input.ContentType),
	})
	if f.failAt > 0 && len(f.calls) == f.failAt {
		return nil, errors.New("upload failed")
	}

	return &manager.UploadOutput{}, nil
}

func TestUploadThumbnailsUploadsStoryboardBeforeVTT(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"storyboard.jpg", "storyboard.vtt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	uploader := &fakeUploader{}
	client := &Client{
		streaming: &bucketClient{bucket: "streaming"},
		uploader:  uploader,
		hlsPrefix: "hls",
		log:       slog.Default(),
	}

	if err := client.UploadThumbnails(context.Background(), dir, "org-1", "media-1"); err != nil {
		t.Fatal(err)
	}

	want := []uploadCall{
		{key: "hls/org-1/media-1/thumbnails/storyboard.jpg", contentType: "image/jpeg"},
		{key: "hls/org-1/media-1/thumbnails/storyboard.vtt", contentType: "text/vtt; charset=utf-8"},
	}
	if len(uploader.calls) != len(want) {
		t.Fatalf("upload calls = %+v", uploader.calls)
	}
	for index := range want {
		if uploader.calls[index] != want[index] {
			t.Errorf("upload call %d = %+v, want %+v", index, uploader.calls[index], want[index])
		}
	}
}

func TestUploadThumbnailsDoesNotPublishVTTAfterStoryboardFailure(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"storyboard.jpg", "storyboard.vtt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	uploader := &fakeUploader{failAt: 1}
	client := &Client{
		streaming: &bucketClient{bucket: "streaming"},
		uploader:  uploader,
		hlsPrefix: "hls",
		log:       slog.Default(),
	}

	if err := client.UploadThumbnails(context.Background(), dir, "org-1", "media-1"); err == nil {
		t.Fatal("expected upload failure")
	}
	if len(uploader.calls) != 1 || uploader.calls[0].key != "hls/org-1/media-1/thumbnails/storyboard.jpg" {
		t.Fatalf("unexpected upload calls: %+v", uploader.calls)
	}
}

func TestUploadPosterUsesMediaRootThumbnailPath(t *testing.T) {
	dir := t.TempDir()
	posterPath := filepath.Join(dir, "thumbnail.jpg")
	if err := os.WriteFile(posterPath, []byte("poster"), 0o644); err != nil {
		t.Fatal(err)
	}

	uploader := &fakeUploader{}
	client := &Client{
		streaming: &bucketClient{bucket: "streaming"},
		uploader:  uploader,
		hlsPrefix: "hls",
		log:       slog.Default(),
	}

	if err := client.UploadPoster(context.Background(), posterPath, "org-1", "media-1"); err != nil {
		t.Fatal(err)
	}
	if len(uploader.calls) != 1 {
		t.Fatalf("upload calls = %+v", uploader.calls)
	}
	want := uploadCall{key: "hls/org-1/media-1/thumbnail.jpg", contentType: "image/jpeg"}
	if uploader.calls[0] != want {
		t.Fatalf("upload call = %+v, want %+v", uploader.calls[0], want)
	}
}
