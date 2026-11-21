package internal

import (
	"time"

	"gorm.io/gorm"
)

type ProgressStatus string

const (
	QUEUED    ProgressStatus = "queued"
	COMPLETED ProgressStatus = "completed"
	FAILED    ProgressStatus = "failed"
)

type Action struct {
	Id        uint           `gorm:"primaryKey" json:"id"`
	Status    ProgressStatus `json:"status"`
	Source    string         `json:"source"`
	UserId    uint           `json:"user_id"`
	User      User           `gorm:"foreignKey:UserId;references:Id" json:"user"`
	Prompt    Prompt         `gorm:"foreignKey:ActionId;references:Id" json:"prompt"`
	Answer    PromptAnswer   `gorm:"foreignKey:ActionId;references:Id" json:"answer"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

type User struct {
	Id             uint      `gorm:"primaryKey" json:"id"`
	Source         string    `gorm:"uniqueIndex:idx_user_source_uid" json:"source"`
	Name           string    `json:"name"`
	UserIdOnSource string    `gorm:"uniqueIndex:idx_user_source_uid" json:"user_id_on_source"`
	IsVerified     bool      `gorm:"default:false" json:"is_verified"`
	IsBlocked      bool      `gorm:"default:false" json:"is_blocked"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Prompt struct {
	ActionId uint   `gorm:"primaryKey;autoIncrement:false" json:"action_id"`
	Text     string `json:"text"`
}

type PromptAnswer struct {
	ActionId    uint      `gorm:"primaryKey;autoIncrement:false" json:"action_id"`
	Source      string    `json:"source"`
	Text        string    `json:"text"`
	CreatedAt   time.Time `json:"created_at"`
	PostSuccess bool      `json:"post_success"`
}
