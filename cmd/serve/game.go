package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"sync"
)

func proxyGame(ctx context.Context) error {
	ln, err := net.Listen("tcp4", net.JoinHostPort("localhost", gameProxyPort))
	if err != nil {
		return err
	}
	defer ln.Close()
	logger.Infow("started game proxy",
		"address", ln.Addr().String(),
	)

	errCh := make(chan error, 1)
	connCh := make(chan net.Conn)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				errCh <- err
				return
			}
			connCh <- conn
		}
	}()

	var loopErr error
	wg := sync.WaitGroup{}
LOOP:
	for {
		select {
		case conn := <-connCh:
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := handleGameConn(ctx, conn)
				if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
					log.Printf("error while handling game connection: %s", err)
				}
			}()
		case err := <-errCh:
			loopErr = err
			break LOOP
		case <-ctx.Done():
			loopErr = ctx.Err()
			break LOOP
		}
	}
	wg.Wait()
	return loopErr
}

// TODO
func handleGameConn(ctx context.Context, conn net.Conn) error {
	return nil
}
