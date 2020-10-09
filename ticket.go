package d1proxy

import (
	"time"
)

type Ticket struct {
	Host     string
	Port     string
	Original string

	IssuedAt time.Time
	ServerId int
}
