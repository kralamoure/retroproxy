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

	"github.com/kralamoure/retroproto"
	"github.com/kralamoure/retroproto/msgcli"
	"github.com/kralamoure/retroproto/msgsvr"
	"go.uber.org/zap"

	"github.com/kralamoure/retroproxy"
)

type session struct {
	proxy      *Proxy
	clientConn *net.TCPConn
	serverConn *net.TCPConn

	ticket              retroproxy.Ticket
	ticketCh            chan retroproxy.Ticket
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

		conn, err := net.DialTimeout("tcp4", net.JoinHostPort(t.Host, t.Port), 3*time.Second)
		if err != nil {
			return err
		}
		defer conn.Close()
		tcpConn, ok := conn.(*net.TCPConn)
		if !ok {
			return errors.New("could not assert server connection as a tcp connection")
		}
		s.proxy.logger.Info("connected to server",
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

func (s *session) handlePktFromServer(ctx context.Context, packet string) error {
	id, ok := retroproto.MsgSvrIdByPkt(packet)
	name, _ := retroproto.MsgSvrNameByID(id)
	s.proxy.logger.Info("received packet from server",
		zap.String("server_address", s.serverConn.RemoteAddr().String()),
		zap.String("client_address", s.clientConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", packet),
	)
	if ok {
		switch id {
		case retroproto.AksHelloGame:
			err := s.sendMsgToServer(&msgcli.AccountSendTicket{Ticket: s.ticket.Original})
			if err != nil {
				return err
			}
			return nil
		case retroproto.GameMovement:
			extra := strings.TrimPrefix(packet, string(id))

			msg := &msgsvr.GameMovement{}
			err := msg.Deserialize(extra)
			if err != nil {
				return err
			}

			for _, sprite := range msg.Sprites {
				if sprite.Fight {
					continue
				}
				if sprite.Type < 1 {
					continue
				}
				s.proxy.logger.Debug("character spotted",
					zap.String("character_name", sprite.Character.Name),
					zap.Int("character_level", sprite.Character.Level),
				)
			}
		}
	}

	s.sendPktToClient(packet)

	return nil
}

func (s *session) handlePktFromClient(ctx context.Context, rawPacket string) error {
	packet := rawPacket

	// unknownToken seems to wrap a base64 encoded string sent by the client as the prefix of some types of packet.
	// That mechanism was introduced by a new client version, but it's not clear to me which version or why.
	// Maybe it's some kind of signature mechanism.
	const unknownToken = "ù"
	if strings.HasPrefix(packet, unknownToken) {
		const index = 2
		substrings := strings.SplitN(packet, unknownToken, index+1)
		if len(substrings) != index+1 {
			s.proxy.logger.Warn("invalid packet but won't discard it")
		} else {
			packet = substrings[index]
		}
	}

	id, ok := retroproto.MsgCliIdByPkt(packet)
	name, _ := retroproto.MsgCliNameByID(id)
	s.proxy.logger.Info("received packet from client",
		zap.String("client_address", s.clientConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", packet),
		zap.String("raw_packet", rawPacket),
	)
	if s.firstPkt && !ok {
		return errors.New("invalid first packet")
	}
	if ok {
		extra := strings.TrimPrefix(packet, string(id))
		switch id {
		case retroproto.AccountSendTicket:
			if !s.firstPkt {
				return errors.New("unexpected packet")
			}
			msg := &msgcli.AccountSendTicket{}
			err := msg.Deserialize(extra)
			if err != nil {
				return err
			}

			t, ok := s.proxy.storer.UseTicket(msg.Ticket)
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
	s.sendPktToServer(rawPacket)
	return nil
}

func (s *session) sendMsgToServer(msg retroproto.MsgCli) error {
	pkt, err := msg.Serialized()
	if err != nil {
		return err
	}
	s.sendPktToServer(fmt.Sprint(msg.MessageId(), pkt))
	return nil
}

func (s *session) sendMsgToClient(msg retroproto.MsgSvr) error {
	pkt, err := msg.Serialized()
	if err != nil {
		return err
	}
	s.sendPktToClient(fmt.Sprint(msg.MessageId(), pkt))
	return nil
}

func (s *session) sendPktToServer(rawPacket string) {
	packet := rawPacket

	// unknownToken seems to wrap a base64 encoded string sent by the client as the prefix of some types of packet.
	// That mechanism was introduced by a new client version, but it's not clear to me which version or why.
	// Maybe it's some kind of signature mechanism.
	const unknownToken = "ù"
	if strings.HasPrefix(packet, unknownToken) {
		const index = 2
		substrings := strings.SplitN(packet, unknownToken, index+1)
		if len(substrings) != index+1 {
			s.proxy.logger.Warn("invalid packet but won't discard it")
		} else {
			packet = substrings[index]
		}
	}

	id, _ := retroproto.MsgCliIdByPkt(packet)
	name, _ := retroproto.MsgCliNameByID(id)
	s.proxy.logger.Info("sent packet to server",
		zap.String("server_address", s.serverConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", packet),
		zap.String("raw_packet", rawPacket),
	)
	fmt.Fprint(s.serverConn, rawPacket+"\n\x00")
}

func (s *session) sendPktToClient(pkt string) {
	id, _ := retroproto.MsgSvrIdByPkt(pkt)
	name, _ := retroproto.MsgSvrNameByID(id)
	s.proxy.logger.Info("sent packet to client",
		zap.String("client_address", s.clientConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)
	fmt.Fprint(s.clientConn, pkt+"\x00")
}
