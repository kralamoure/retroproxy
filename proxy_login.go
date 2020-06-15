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
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := p.acceptClientConns(ctx)
		if err != nil {
			select {
			case errCh <- fmt.Errorf("error while accepting login client connections: %w", err):
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

func (p *loginProxy) acceptClientConns(ctx context.Context) error {
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
				logger.Debugf("error while handling login client connection: %s", err)
			}
		}()
	}
}

func (p *loginProxy) handleClientConn(ctx context.Context, conn net.Conn) error {
	wg := sync.WaitGroup{}
	defer wg.Wait()

	defer func() {
		conn.Close()
		logger.Infow("client disconnected",
			"client_address", conn.RemoteAddr().String(),
		)
	}()
	logger.Infow("new connection from login client",
		"client_address", conn.RemoteAddr().String(),
	)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	s := &loginSession{
		clientConn: conn,
		serverIdCh: make(chan int),
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
		err := s.receivePktsFromServer(ctx)
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
