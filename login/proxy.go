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
	logger     *zap.Logger
	addr       *net.TCPAddr
	serverAddr *net.TCPAddr
	repo       d1sniff.Repo

	gameHost string
	gamePort string

	ln       *net.TCPListener
	sessions map[*session]struct{}
	mu       sync.Mutex
}

func NewProxy(addr, serverAddr, gameAddr string, repo d1sniff.Repo, logger *zap.Logger) (*Proxy, error) {
	if repo == nil {
		return nil, errors.New("repository is nil")
	}
	if logger == nil {
		logger = zap.NewNop()
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
		logger:     logger,
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
	defer func() {
		ln.Close()
		p.logger.Info("stopped listening",
			zap.String("address", ln.Addr().String()),
		)
	}()
	p.logger.Info("listening",
		zap.String("address", ln.Addr().String()),
	)
	p.ln = ln

	errCh := make(chan error)
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := p.acceptLoop(ctx)
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

func (p *Proxy) acceptLoop(ctx context.Context) error {
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
			if err != nil && !(errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || errors.Is(err, errEndOfService)) {
				p.logger.Debug("error while handling client connection",
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
		p.logger.Info("client disconnected",
			zap.String("client_address", conn.RemoteAddr().String()),
		)
	}()
	p.logger.Info("client connected",
		zap.String("client_address", conn.RemoteAddr().String()),
	)

	s := &session{
		proxy:      p,
		clientConn: conn,
		serverIdCh: make(chan int),
	}

	p.trackSession(s, true)
	defer p.trackSession(s, false)

	serverConn, err := net.DialTimeout("tcp", p.serverAddr.String(), 3*time.Second)
	if err != nil {
		return err
	}
	defer serverConn.Close()
	tcpServerConn, ok := serverConn.(*net.TCPConn)
	if !ok {
		return errors.New("could not assert server connection as a tcp connection")
	}
	p.logger.Info("connected to server",
		zap.String("client_address", conn.RemoteAddr().String()),
		zap.String("server_address", tcpServerConn.RemoteAddr().String()),
	)
	s.serverConn = tcpServerConn

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

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

func (p *Proxy) trackSession(s *session, add bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if add {
		if p.sessions == nil {
			p.sessions = make(map[*session]struct{})
		}
		p.sessions[s] = struct{}{}
	} else {
		delete(p.sessions, s)
	}
}
