package models

import (
	"strings"
	"time"
)

// Email represents an email record in the messaging system.
// Used for contact form submissions, auth verification, password reset, and 2FA emails.
type Email struct {
	ID             int64      `json:"id" gorm:"primaryKey"`
	Type           string     `json:"type" gorm:"column:type;default:contact_form"`
	Name           *string    `json:"name,omitempty" gorm:"column:name"`
	SenderEmail    *string    `json:"senderEmail,omitempty" gorm:"column:email"`
	RecipientEmail *string    `json:"recipientEmail,omitempty" gorm:"column:recipient_email"`
	Subject        string     `json:"subject" gorm:"column:subject"`
	Message        string     `json:"message" gorm:"column:message"`
	Honeypot       string     `json:"-" gorm:"column:honeypot"`
	Status         string     `json:"status" gorm:"column:status;default:pending"`
	Attempts       int        `json:"attempts" gorm:"column:attempts;default:0"`
	LastError      *string    `json:"lastError,omitempty" gorm:"column:last_error"`
	CreatedAt      time.Time  `json:"createdAt" gorm:"column:created_at"`
	UpdatedAt      time.Time  `json:"updatedAt" gorm:"column:updated_at"`
	SentAt         *time.Time `json:"sentAt,omitempty" gorm:"column:sent_at"`
}

func (Email) TableName() string {
	return "messaging.emails"
}

// Email type constants
const (
	EmailTypeContactForm       = "contact_form"
	EmailTypeEmailVerification = "email_verification"
	EmailTypePasswordReset     = "password_reset"
	EmailType2FACode           = "2fa_code"
)

// Email status constants
const (
	EmailStatusPending = "pending"
	EmailStatusQueued  = "queued"
	EmailStatusSent    = "sent"
	EmailStatusFailed  = "failed"
)

// ValidEmailStatus reports whether s is a known email status value.
func ValidEmailStatus(s string) bool {
	switch s {
	case EmailStatusPending, EmailStatusQueued, EmailStatusSent, EmailStatusFailed:
		return true
	}
	return false
}

// ContactMessageCreate is the DTO for creating a new contact message (public endpoint)
type ContactMessageCreate struct {
	Name     string `json:"name" binding:"required,max=255"`
	Email    string `json:"email" binding:"required,email,max=255"`
	Subject  string `json:"subject" binding:"required,max=500"`
	Message  string `json:"message" binding:"required,max=10000"`
	Honeypot string `json:"website" swaggerignore:"true"`
}

// IsSpam checks if the honeypot field is filled (indicates bot submission)
func (c *ContactMessageCreate) IsSpam() bool {
	return strings.TrimSpace(c.Honeypot) != ""
}

// EmailEvent is the message published to the queue for processing
type EmailEvent struct {
	EmailID int64 `json:"emailId"`
}
