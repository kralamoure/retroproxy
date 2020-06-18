package login

import (
	"context"
	"net"
	"sync"

	"go.uber.org/zap"
)

type Proxy struct {
	addr       *net.TCPAddr
	serverAddr *net.TCPAddr
	ln         *net.TCPListener

	gameHost string
	gamePort string
}

func NewProxy(addr, serverAddr, gameAddr string) (*Proxy, error) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, err
	}
	tcpServerAddr, err := net.ResolveTCPAddr("tcp", serverAddr)
	if err != nil {
		return nil, err
	}
	gameHost, gamePort, err := net.SplitHostPort(gameAddr)
	if err != nil {
		return nil, err
	}
	return &Proxy{
		addr:       tcpAddr,
		serverAddr: tcpServerAddr,
		gameHost:   gameHost,
		gamePort:   gamePort,
	}, nil
}

func (p *Proxy) ListenAndServe(ctx context.Context) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	ln, err := net.ListenTCP("tcp", p.addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	p.ln = ln

	errCh := make(chan error)

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := p.serve(ctx)
		if err != nil {
			select {
			case errCh <- err:
			case <-ctx.Done():
			}
		}
		zap.L().Info("login: serving",
			zap.String("address", ln.Addr().String()),
		)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (p *Proxy) serve(ctx context.Context) error {
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := p.ln.AcceptTCP()
		if err != nil {
			return err
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			err := p.handleClientConn(ctx, conn)
			if err != nil {
				zap.L().Debug("error while handling client connection",
					zap.Error(err),
					zap.String("client_address", conn.RemoteAddr().String()),
				)
			}
		}()
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

	serverConn, err := net.DialTCP("tcp", nil, p.serverAddr)
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
