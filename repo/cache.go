package repo

import (
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/kralamoure/d1sniff"
)

type Cache struct {
	mu      sync.Mutex
	tickets map[string]d1sniff.Ticket
}

func (c *Cache) SetTicket(id string, t d1sniff.Ticket) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tickets == nil {
		c.tickets = make(map[string]d1sniff.Ticket)
	}
	c.tickets[id] = t
	zap.L().Debug("ticket set",
		zap.String("ticket_id", id),
	)
}

func (c *Cache) UseTicket(id string) (d1sniff.Ticket, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tickets[id]
	if ok {
		delete(c.tickets, id)
		zap.L().Debug("ticket used",
			zap.String("ticket_id", id),
		)
	}
	return t, ok
}

func (c *Cache) DeleteOldTickets(maxDur time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for id := range c.tickets {
		deadline := c.tickets[id].IssuedAt.Add(maxDur)
		if now.After(deadline) {
			delete(c.tickets, id)
			zap.L().Debug("old ticket deleted",
				zap.String("ticket_id", id),
			)
		}
	}
}
