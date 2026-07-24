package models

import "time"

// DeliveryAttempt represents an email delivery attempt for a contact message
type DeliveryAttempt struct {
	ID             int64     `json:"id" gorm:"primaryKey"`
	MessageID      int64     `json:"messageId" gorm:"column:message_id;index"`
	RecipientEmail string    `json:"recipientEmail" gorm:"column:recipient_email"`
	Status         string    `json:"status" gorm:"column:status"`
	ErrorCode      *string   `json:"errorCode,omitempty" gorm:"column:error_code"`
	ErrorMessage   *string   `json:"errorMessage,omitempty" gorm:"column:error_message"`
	AttemptedAt    time.Time `json:"attemptedAt" gorm:"column:attempted_at"`
}

func (DeliveryAttempt) TableName() string {
	return "messaging.delivery_attempts"
}

// DeliveryAttemptStatus constants. The table's CHECK constraint allows these two only.
const (
	DeliveryStatusSuccess = "success"
	DeliveryStatusFailed  = "failed"
)
