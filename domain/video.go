package domain

import (
	"time"

	"github.com/asaskevich/govalidator"
)

// Video represents an input media object and its processing jobs.
type Video struct {
	ID         string    `json:"encoded_video_folder" valid:"uuid" gorm:"type:uuid;primaryKey"`
	ResourceID string    `json:"resource_id" valid:"notnull" gorm:"type:varchar(255)"`
	FilePath   string    `json:"file_path" valid:"notnull" gorm:"type:varchar(255)"`
	CreatedAt  time.Time `json:"-" valid:"-"`
	Jobs       []*Job    `json:"-" valid:"-" gorm:"foreignKey:VideoID"`
}

// init configures govalidator to require struct fields by default.
func init() {
	govalidator.SetFieldsRequiredByDefault(true)
}

// NewVideo returns an empty Video instance.
func NewVideo() *Video {
	return &Video{}
}

// Validate checks the Video fields against govalidator tags.
func (video *Video) Validate() error {
	_, err := govalidator.ValidateStruct(video)

	if err != nil {
		return err
	}

	return nil
}
