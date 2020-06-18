package login

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/kralamoure/d1sniff"
)

type Proxy struct {
	addr       *net.TCPAddr
	serverAddr *net.TCPAddr
	ln         *net.TCPListener
	repo       *d1sniff.Repo

	gameHost string
	gamePort string
}

func NewProxy(addr, serverAddr, gameAddr string, repo *d1sniff.Repo) (*Proxy, error) {
	if repo == nil {
		return nil, errors.New("repository is nil")
	}

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
		repo:       repo,
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
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (p *Proxy) serve(ctx context.Context) error {
	defer zap.L().Info("login: stopped serving",
		zap.String("address", p.ln.Addr().String()),
	)
	zap.L().Info("login: serving",
		zap.String("address", p.ln.Addr().String()),
	)
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
			if err != nil && !errors.Is(err, io.EOF) {
				zap.L().Debug("login: error while handling client connection",
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

	serverConn, err := net.DialTimeout("tcp", p.serverAddr.String(), 3*time.Second)
	if err != nil {
		return err
	}
	defer serverConn.Close()
	tcpServerConn, ok := serverConn.(*net.TCPConn)
	if !ok {
		return errors.New("could not cast server connection as a tcp connection")
	}
	zap.L().Info("login: connected to server",
		zap.String("client_address", conn.RemoteAddr().String()),
		zap.String("server_address", tcpServerConn.RemoteAddr().String()),
	)

	s := session{
		proxy:      p,
		clientConn: conn,
		serverConn: tcpServerConn,
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
