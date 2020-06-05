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
}

func useTicket(id string) (ticket, bool) {
	ticketsMu.Lock()
	defer ticketsMu.Unlock()
	t, ok := tickets[id]
	if ok {
		delete(tickets, id)
	}
	return t, ok
}

func deleteOldTickets(d time.Duration) {
	ticketsMu.Lock()
	defer ticketsMu.Unlock()
	now := time.Now()
	for id := range tickets {
		deadline := tickets[id].issuedAt.Add(d)
		if deadline.After(now) {
			logger.Infow("deleted ticket",
				"ticket_id", id,
			)
			delete(tickets, id)
		}
	}
}
