package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/kralamoure/d1proto/msgsvr"
	"go.uber.org/zap"
)

type gameProxy struct {
	ln net.Listener
}

func (p *gameProxy) start(ctx context.Context) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	ln, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", gameProxyPort))
	if err != nil {
		return err
	}
	defer func() {
		ln.Close()
		zap.L().Info("game proxy listener closed",
			zap.String("address", ln.Addr().String()),
		)
	}()
	zap.L().Info("game proxy listening",
		zap.String("address", ln.Addr().String()),
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
	var wg sync.WaitGroup
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
				zap.L().Debug(fmt.Sprintf("error while handling game client connection: %s", err))
			}
		}()
	}
}

func (p *gameProxy) handleClientConn(ctx context.Context, conn net.Conn) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	defer func() {
		conn.Close()
		zap.L().Info("game client disconnected",
			zap.String("client_address", conn.RemoteAddr().String()),
		)
	}()
	zap.L().Info("game client connected",
		zap.String("client_address", conn.RemoteAddr().String()),
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
