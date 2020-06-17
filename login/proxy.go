package login

import (
	"context"
	"fmt"
	"net"
	"sync"

	"go.uber.org/zap"
)

type Proxy struct {
	ln         *net.TCPListener
	serverAddr *net.TCPAddr

	gameHost string
	gamePort string
}

func NewProxy(ln *net.TCPListener, serverAddr, gameAddr string) (*Proxy, error) {
	tcpServerAddr, err := net.ResolveTCPAddr("tcp4", serverAddr)
	if err != nil {
		return nil, err
	}
	gameHost, gamePort, err := net.SplitHostPort(gameAddr)
	if err != nil {
		return nil, err
	}
	return &Proxy{
		serverAddr: tcpServerAddr,
		gameHost:   gameHost,
		gamePort:   gamePort,
		ln:         ln,
	}, nil
}

func (p *Proxy) Serve(ctx context.Context) error {
	var wg sync.WaitGroup
	defer wg.Wait()
	defer p.ln.Close()
	errCh := make(chan error)
	connCh := make(chan *net.TCPConn)
	go func() {
		zap.L().Info("login: serving",
			zap.String("address", p.ln.Addr().String()),
		)
		for {
			conn, err := p.ln.AcceptTCP()
			if err != nil {
				select {
				case errCh <- err:
				case <-ctx.Done():
				}
			}
			connCh <- conn
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return err
		case conn := <-connCh:
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := p.handleClientConn(ctx, conn)
				if err != nil {
					zap.L().Debug(fmt.Sprintf("login: error while handling client connection: %s", err),
						zap.String("client_address", conn.RemoteAddr().String()),
					)
				}
			}()
		}
	}
}

func (p *Proxy) handleClientConn(ctx context.Context, conn *net.TCPConn) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	defer func() {
		conn.Close()
		zap.L().Info("login: client disconnected",
			zap.String("client_address", conn.RemoteAddr().String()),
		)
	}()
	zap.L().Info("login: client connected",
		zap.String("client_address", conn.RemoteAddr().String()),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverConn, err := net.DialTCP("tcp4", nil, p.serverAddr)
	if err != nil {
		return err
	}
	defer serverConn.Close()
	zap.L().Info("login: connected to server",
		zap.String("client_address", conn.RemoteAddr().String()),
		zap.String("server_address", serverConn.RemoteAddr().String()),
	)

	s := session{
		proxy:      p,
		clientConn: conn,
		serverConn: serverConn,
		serverIdCh: make(chan int),
	}

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
