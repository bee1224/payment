package domain

import "time"

type Merchant struct {
	ID          int64
	Code        string
	Name        string
	APIKey      string
	APISecret   string
	Status      string
	CallbackURL string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
