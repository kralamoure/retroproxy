package retroproxy

import (
	"context"
	"time"
)

type Storer interface {
	SetTicket(id string, t Ticket)
	UseTicket(id string) (Ticket, bool)
	DeleteOldTickets(maxDur time.Duration)
}

func DeleteOldTicketsLoop(ctx context.Context, r Storer, maxDur time.Duration) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.DeleteOldTickets(maxDur)
		case <-ctx.Done():
			return
		}
	}
}
