# models

Shared GORM database models for portfolio services.

## Schema Domains

### portfolio.*

- `Profile` → `StorageFile` (avatar, resume)
- `WorkExperience`
- `Certification`
- `Skill`
- `PortfolioProject` → `StorageFile`, `Skill` (many-to-many via project_technologies)

### miniatures.*

- `MiniatureTheme` → `MiniatureProject[]`, `StorageFile` (cover)
- `MiniatureProject` → `MiniatureTheme`, `MiniatureFile[]`,
  `MiniatureProjectTechnique[]`, `MiniatureProjectPaint[]`
- `MiniatureFile` → `StorageFile`
- `MiniatureTechnique` (cl_techniques catalog)
- `MiniaturePaint` (cl_paints catalog)
- `MiniatureProjectTechnique` → `MiniatureTechnique` (junction)
- `MiniatureProjectPaint` → `MiniaturePaint` (junction)

### storage.*

- `StorageFile` - File metadata for MinIO storage

### messaging.*

- `ContactMessage` - Contact form submissions with status tracking
- `Recipient` - Email recipients for notifications
- `DeliveryAttempt` - Email delivery tracking

### auth.*

- `User` - User accounts
- `ActionLog` - Audit log entries

## Usage

```go
import "github.com/adeptry-app/go-common/models"

// Load profile with associated files
var profile models.Profile
db.Preload("AvatarFile").Preload("ResumeFile").First(&profile)

// Load miniature project with all associations
var project models.MiniatureProject
db.Preload("Theme").Preload("MiniatureFiles.File").Preload("Techniques.Technique").First(&project)
```
