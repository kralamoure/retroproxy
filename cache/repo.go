package cache

import (
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/kralamoure/d1sniff"
)

type Repo struct {
	mu      sync.Mutex
	tickets map[string]d1sniff.Ticket
}

func (r *Repo) SetTicket(id string, t d1sniff.Ticket) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tickets == nil {
		r.tickets = make(map[string]d1sniff.Ticket)
	}
	r.tickets[id] = t
	zap.L().Debug("ticket set",
		zap.String("ticket_id", id),
	)
}

func (r *Repo) UseTicket(id string) (d1sniff.Ticket, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tickets[id]
	if ok {
		delete(r.tickets, id)
		zap.L().Debug("ticket used",
			zap.String("ticket_id", id),
		)
	}
	return t, ok
}

func (r *Repo) DeleteOldTickets(maxDur time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for id := range r.tickets {
		deadline := r.tickets[id].IssuedAt.Add(maxDur)
		if now.After(deadline) {
			delete(r.tickets, id)
			zap.L().Debug("old ticket deleted",
				zap.String("ticket_id", id),
			)
		}
	}
}
