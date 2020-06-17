package main

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

type ticket struct {
	host     string
	port     string
	original string

	issuedAt time.Time
	serverId int
}

var (
	tickets   = make(map[string]ticket)
	ticketsMu sync.Mutex
)

func setTicket(id string, t ticket) {
	ticketsMu.Lock()
	defer ticketsMu.Unlock()
	tickets[id] = t
	zap.L().Debug("ticket set",
		zap.String("ticket_id", id),
	)
}

func useTicket(id string) (ticket, bool) {
	ticketsMu.Lock()
	defer ticketsMu.Unlock()
	t, ok := tickets[id]
	if ok {
		delete(tickets, id)
		zap.L().Debug("ticket used",
			zap.String("ticket_id", id),
		)
	}
	return t, ok
}

func deleteOldTickets(maxDur time.Duration) {
	ticketsMu.Lock()
	defer ticketsMu.Unlock()
	now := time.Now()
	for id := range tickets {
		deadline := tickets[id].issuedAt.Add(maxDur)
		if now.After(deadline) {
			delete(tickets, id)
			zap.L().Debug("old ticket deleted",
				zap.String("ticket_id", id),
			)
		}
	}
}
