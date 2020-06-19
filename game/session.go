package game

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/kralamoure/d1proto"
	"github.com/kralamoure/d1proto/msgcli"
	"github.com/kralamoure/d1proto/msgsvr"
	"go.uber.org/zap"

	"github.com/kralamoure/d1sniff"
)

type session struct {
	proxy      *Proxy
	clientConn *net.TCPConn
	serverConn *net.TCPConn

	ticket              d1sniff.Ticket
	ticketCh            chan d1sniff.Ticket
	connectedToServerCh chan struct{}

	firstPkt bool
}

func (s *session) connectToServer(ctx context.Context) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	errCh := make(chan error)

	select {
	case t := <-s.ticketCh:
		s.ticket = t

		conn, err := net.DialTimeout("tcp", net.JoinHostPort(t.Host, t.Port), 3*time.Second)
		if err != nil {
			return err
		}
		defer conn.Close()
		tcpConn, ok := conn.(*net.TCPConn)
		if !ok {
			return errors.New("could not assert server connection as a tcp connection")
		}
		zap.L().Info("connected to game server",
			zap.String("local_address", tcpConn.LocalAddr().String()),
			zap.String("server_address", tcpConn.RemoteAddr().String()),
			zap.String("client_address", s.clientConn.RemoteAddr().String()),
		)
		s.serverConn = tcpConn
		close(s.connectedToServerCh)

		wg.Add(1)
		go func() {
			defer wg.Done()
			err = s.receivePktsFromServer(ctx)
			if err != nil {
				select {
				case errCh <- err:
				case <-ctx.Done():
				}
			}
		}()
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *session) receivePktsFromServer(ctx context.Context) error {
	rd := bufio.NewReader(s.serverConn)
	for {
		pkt, err := rd.ReadString('\x00')
		if err != nil {
			return err
		}
		pkt = strings.TrimSuffix(pkt, "\x00")
		if pkt == "" {
			continue
		}
		err = s.handlePktFromServer(ctx, pkt)
		if err != nil {
			return err
		}
	}
}

func (s *session) receivePktsFromClient(ctx context.Context) error {
	rd := bufio.NewReader(s.clientConn)
	for {
		pkt, err := rd.ReadString('\x00')
		if err != nil {
			return err
		}
		pkt = strings.TrimSuffix(pkt, "\n\x00")
		if pkt == "" {
			continue
		}
		err = s.handlePktFromClient(ctx, pkt)
		s.firstPkt = false
		if err != nil {
			return err
		}
	}
}

func (s *session) handlePktFromServer(ctx context.Context, pkt string) error {
	id, ok := d1proto.MsgSvrIdByPkt(pkt)
	name, _ := d1proto.MsgSvrNameByID(id)
	zap.L().Info("game: received packet from server",
		zap.String("server_address", s.serverConn.RemoteAddr().String()),
		zap.String("client_address", s.clientConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)
	if ok {
		switch id {
		case d1proto.AksHelloGame:
			err := s.sendMsgToServer(&msgcli.AccountSendTicket{Ticket: s.ticket.Original})
			if err != nil {
				return err
			}
			return nil
		}
	}
	s.sendPktToClient(pkt)
	return nil
}

func (s *session) handlePktFromClient(ctx context.Context, pkt string) error {
	id, ok := d1proto.MsgCliIdByPkt(pkt)
	name, _ := d1proto.MsgCliNameByID(id)
	zap.L().Info("game: received packet from client",
		zap.String("client_address", s.clientConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)
	if s.firstPkt && !ok {
		return errors.New("invalid first packet")
	}
	if ok {
		extra := strings.TrimPrefix(pkt, string(id))
		switch id {
		case d1proto.AccountSendTicket:
			if !s.firstPkt {
				return errors.New("unexpected packet")
			}
			msg := &msgcli.AccountSendTicket{}
			err := msg.Deserialize(extra)
			if err != nil {
				return err
			}

			t, ok := s.proxy.repo.UseTicket(msg.Ticket)
			if !ok {
				err := s.sendMsgToClient(&msgsvr.AccountTicketResponseError{})
				if err != nil {
					return err
				}
				return errors.New("ticket not found")
			}

			select {
			case s.ticketCh <- t:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}
	}
	select {
	case <-s.connectedToServerCh:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.sendPktToServer(pkt)
	return nil
}

func (s *session) sendMsgToServer(msg d1proto.MsgCli) error {
	pkt, err := msg.Serialized()
	if err != nil {
		return err
	}
	s.sendPktToServer(fmt.Sprint(msg.ProtocolId(), pkt))
	return nil
}

func (s *session) sendMsgToClient(msg d1proto.MsgSvr) error {
	pkt, err := msg.Serialized()
	if err != nil {
		return err
	}
	s.sendPktToClient(fmt.Sprint(msg.ProtocolId(), pkt))
	return nil
}

func (s *session) sendPktToServer(pkt string) {
	id, _ := d1proto.MsgCliIdByPkt(pkt)
	name, _ := d1proto.MsgCliNameByID(id)
	zap.L().Info("game: sent packet to server",
		zap.String("server_address", s.serverConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)
	fmt.Fprint(s.serverConn, pkt+"\n\x00")
}

func (s *session) sendPktToClient(pkt string) {
	id, _ := d1proto.MsgSvrIdByPkt(pkt)
	name, _ := d1proto.MsgSvrNameByID(id)
	zap.L().Info("game: sent packet to client",
		zap.String("client_address", s.clientConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)
	fmt.Fprint(s.clientConn, pkt+"\x00")
}
