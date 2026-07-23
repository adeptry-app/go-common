# utils

Common utility functions for file URL generation.

## Usage

```go
import "github.com/adeptry-app/go-common/utils"

// Build file URL: {filesAPIURL}/files/{fileType}/{s3Key}
url := utils.BuildFileURL("http://localhost:8085/api/v1", "image", "avatars/user123.jpg")

// Populate URL field on StorageFile model
utils.PopulateFileURL(file, filesAPIURL)

// Convert MiniatureFiles to simplified Image slice for frontend
images := utils.ConvertMiniatureFilesToImages(miniatureFiles, filesAPIURL)
```
