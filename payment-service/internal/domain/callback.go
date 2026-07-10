package domain

import "time"

type DepositCallback struct {
	ID        int64
	OrderID   int64
	Payload   []byte
	CreatedAt time.Time
}
