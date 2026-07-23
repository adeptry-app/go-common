package utils

import (
	"fmt"

	"github.com/adeptry-app/go-common/models"
)

// BuildFileURL constructs the full URL for a file stored in MinIO/S3
// Format: {filesAPIURL}/files/{fileType}/{s3Key}
// Example: http://localhost:8085/api/v1/files/image/avatars/user123.jpg
func BuildFileURL(filesAPIURL, fileType, s3Key string) string {
	if filesAPIURL == "" || s3Key == "" {
		return ""
	}
	return fmt.Sprintf("%s/files/%s/%s", filesAPIURL, fileType, s3Key)
}

// PopulateFileURL sets the URL field on a StorageFile if it's not nil
func PopulateFileURL(file *models.StorageFile, filesAPIURL string) {
	if file != nil && file.S3Key != "" {
		file.URL = BuildFileURL(filesAPIURL, file.FileType, file.S3Key)
	}
}

// ConvertMiniatureFilesToImages transforms MiniatureFile slice to simplified Image slice for frontend
func ConvertMiniatureFilesToImages(files []models.MiniatureFile, filesAPIURL string) []models.Image {
	images := make([]models.Image, 0, len(files))
	for _, file := range files {
		if file.File != nil {
			images = append(images, models.Image{
				ID:      file.ID,
				URL:     BuildFileURL(filesAPIURL, file.File.FileType, file.File.S3Key),
				Caption: file.Caption,
			})
		}
	}
	return images
}
