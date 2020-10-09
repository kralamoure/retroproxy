package game

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/kralamoure/d1proto/msgsvr"
	"go.uber.org/zap"

	"github.com/kralamoure/d1proxy"
)

type Proxy struct {
	logger *zap.Logger
	addr   *net.TCPAddr
	repo   d1proxy.Repo

	ln       *net.TCPListener
	sessions map[*session]struct{}
	mu       sync.Mutex
}

func NewProxy(addr string, repo d1proxy.Repo, logger *zap.Logger) (*Proxy, error) {
	if repo == nil {
		return nil, errors.New("repository is nil")
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	tcpAddr, err := net.ResolveTCPAddr("tcp4", addr)
	if err != nil {
		return nil, err
	}
	return &Proxy{
		logger: logger,
		addr:   tcpAddr,
		repo:   repo,
	}, nil
}

func (p *Proxy) ListenAndServe(ctx context.Context) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	ln, err := net.ListenTCP("tcp4", p.addr)
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
			if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
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
		proxy:               p,
		clientConn:          conn,
		ticketCh:            make(chan d1proxy.Ticket),
		connectedToServerCh: make(chan struct{}),
		firstPkt:            true,
	}

	p.trackSession(s, true)
	defer p.trackSession(s, false)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

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

	err := s.sendMsgToClient(&msgsvr.AksHelloGame{})
	if err != nil {
		return err
	}

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
