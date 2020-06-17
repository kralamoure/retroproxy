package d1sniff

import (
	"time"

	"go.uber.org/zap"
)

type Ticket struct {
	Host     string
	Port     string
	Original string

	IssuedAt time.Time
	ServerId int
}

var tickets = make(map[string]Ticket)

func SetTicket(id string, t Ticket) {
	mu.Lock()
	defer mu.Unlock()
	tickets[id] = t
	zap.L().Debug("ticket set",
		zap.String("ticket_id", id),
	)
}

func UseTicket(id string) (Ticket, bool) {
	mu.Lock()
	defer mu.Unlock()
	t, ok := tickets[id]
	if ok {
		delete(tickets, id)
		zap.L().Debug("ticket used",
			zap.String("ticket_id", id),
		)
	}
	return t, ok
}

func DeleteOldTickets(maxDur time.Duration) {
	mu.Lock()
	defer mu.Unlock()
	now := time.Now()
	for id := range tickets {
		deadline := tickets[id].IssuedAt.Add(maxDur)
		if now.After(deadline) {
			delete(tickets, id)
			zap.L().Debug("old ticket deleted",
				zap.String("ticket_id", id),
			)
		}
	}
}
