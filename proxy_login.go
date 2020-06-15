package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

type loginProxy struct {
	ln net.Listener
}

func (p *loginProxy) start(ctx context.Context) error {
	wg := sync.WaitGroup{}
	defer wg.Wait()

	ln, err := net.Listen("tcp4", net.JoinHostPort("localhost", loginProxyPort))
	if err != nil {
		return err
	}
	defer ln.Close()
	logger.Infow("started login proxy",
		"address", ln.Addr().String(),
	)
	p.ln = ln

	errCh := make(chan error)
	connCh := make(chan net.Conn)
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := p.acceptConns(connCh)
		if err != nil {
			select {
			case errCh <- fmt.Errorf("error while accepting login proxy conns: %w", err):
			case <-ctx.Done():
			}
		}
	}()

	for {
		select {
		case conn := <-connCh:
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := p.handleClientConn(ctx, conn)
				if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
					logger.Debugf("error while handling login connection: %s", err)
				}
			}()
		case err := <-errCh:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (p *loginProxy) acceptConns(connCh chan<- net.Conn) error {
	for {
		conn, err := p.ln.Accept()
		if err != nil {
			return err
		}
		connCh <- conn
	}
}

func (p *loginProxy) handleClientConn(ctx context.Context, conn net.Conn) error {
	wg := sync.WaitGroup{}
	defer wg.Wait()

	defer conn.Close()
	logger.Infow("new connection from login client",
		"client_address", conn.RemoteAddr().String(),
	)

	s := &loginSession{
		clientConn: conn,
	}

	serverConn, err := net.Dial("tcp4", loginServerAddress)
	if err != nil {
		return err
	}
	defer serverConn.Close()
	logger.Infow("connected to login server",
		"local_address", serverConn.LocalAddr().String(),
		"server_address", serverConn.RemoteAddr().String(),
		"client_address", conn.RemoteAddr().String(),
	)
	s.serverConn = serverConn

	errCh := make(chan error)

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := s.receivePktsFromServer()
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
		err := s.receivePktsFromClient()
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
