package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/kralamoure/d1proto/msgsvr"
)

type gameProxy struct {
	ln net.Listener
}

func (p *gameProxy) start(ctx context.Context) error {
	wg := sync.WaitGroup{}
	defer wg.Wait()

	ln, err := net.Listen("tcp4", net.JoinHostPort("localhost", gameProxyPort))
	if err != nil {
		return err
	}
	defer func() {
		ln.Close()
		logger.Infow("game proxy listener closed",
			"address", ln.Addr().String(),
		)
	}()
	logger.Infow("game proxy listening",
		"address", ln.Addr().String(),
	)
	p.ln = ln

	errCh := make(chan error)
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := p.acceptClientConns(ctx)
		if err != nil {
			select {
			case errCh <- fmt.Errorf("error while accepting game client connections: %w", err):
			case <-ctx.Done():
			}
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *gameProxy) acceptClientConns(ctx context.Context) error {
	wg := sync.WaitGroup{}
	defer wg.Wait()

	for {
		conn, err := p.ln.Accept()
		if err != nil {
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := p.handleClientConn(ctx, conn)
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
				logger.Debugf("error while handling game client connection: %s", err)
			}
		}()
	}
}

func (p *gameProxy) handleClientConn(ctx context.Context, conn net.Conn) error {
	wg := sync.WaitGroup{}
	defer wg.Wait()

	defer func() {
		conn.Close()
		logger.Infow("game client disconnected",
			"client_address", conn.RemoteAddr().String(),
		)
	}()
	logger.Infow("game client connected",
		"client_address", conn.RemoteAddr().String(),
	)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	s := &gameSession{
		clientConn: conn,
		ticketCh:   make(chan ticket),
	}

	err := s.sendMsgToClient(&msgsvr.AksHelloGame{})
	if err != nil {
		return err
	}

	errCh := make(chan error)

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := s.connectToServer(ctx)
		if err != nil {
			select {
			case errCh <- err:
			case <-ctx.Done():
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := s.receivePktsFromClient(ctx)
		if err != nil {
			select {
			case errCh <- err:
			case <-ctx.Done():
			}
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
