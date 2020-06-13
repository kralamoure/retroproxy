package main

import (
	"sync"
	"time"
)

type ticket struct {
	host             string
	port             string
	originalTicketId string

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
	logger.Debugw("set ticket",
		"ticket_id", id,
	)
}

func useTicket(id string) (ticket, bool) {
	ticketsMu.Lock()
	defer ticketsMu.Unlock()
	t, ok := tickets[id]
	if ok {
		delete(tickets, id)
		logger.Debugw("used ticket",
			"ticket_id", id,
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
			logger.Debugw("deleted old ticket",
				"ticket_id", id,
			)
		}
	}
}
