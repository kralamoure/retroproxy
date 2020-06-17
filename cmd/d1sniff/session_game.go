package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/kralamoure/d1proto"
	"github.com/kralamoure/d1proto/msgcli"
	"github.com/kralamoure/d1proto/msgsvr"
	"go.uber.org/zap"
)

type gameSession struct {
	clientConn net.Conn
	serverConn net.Conn

	ticket   ticket
	ticketCh chan ticket
}

func (s *gameSession) connectToServer(ctx context.Context) error {
	errCh := make(chan error)

	select {
	case t := <-s.ticketCh:
		s.ticket = t

		addr := net.JoinHostPort(t.host, t.port)
		conn, err := net.Dial("tcp4", addr)
		if err != nil {
			return err
		}
		defer conn.Close()
		zap.L().Info("connected to game server",
			zap.String("local_address", conn.LocalAddr().String()),
			zap.String("server_address", conn.RemoteAddr().String()),
			zap.String("client_address", s.clientConn.RemoteAddr().String()),
		)
		s.serverConn = conn

		go func() {
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

func (s *gameSession) receivePktsFromServer(ctx context.Context) error {
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

func (s *gameSession) receivePktsFromClient(ctx context.Context) error {
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
		if err != nil {
			return err
		}
	}
}

func (s *gameSession) handlePktFromServer(ctx context.Context, pkt string) error {
	id, ok := d1proto.MsgSvrIdByPkt(pkt)
	name, _ := d1proto.MsgSvrNameByID(id)
	zap.L().Info("received packet from game server",
		zap.String("server_address", s.serverConn.RemoteAddr().String()),
		zap.String("client_address", s.clientConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)
	if ok {
		switch id {
		case d1proto.AksHelloGame:
			err := s.sendMsgToServer(&msgcli.AccountSendTicket{Ticket: s.ticket.original})
			if err != nil {
				return err
			}
			return nil
		}
	}
	s.sendPktToClient(pkt)
	return nil
}

func (s *gameSession) handlePktFromClient(ctx context.Context, pkt string) error {
	id, ok := d1proto.MsgCliIdByPkt(pkt)
	name, _ := d1proto.MsgCliNameByID(id)
	zap.L().Info("received packet from game client",
		zap.String("client_address", s.clientConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)
	if ok {
		extra := strings.TrimPrefix(pkt, string(id))
		switch id {
		case d1proto.AccountSendTicket:
			msg := &msgcli.AccountSendTicket{}
			err := msg.Deserialize(extra)
			if err != nil {
				return err
			}

			t, ok := useTicket(msg.Ticket)
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
	s.sendPktToServer(pkt)
	return nil
}

func (s *gameSession) sendMsgToServer(msg d1proto.MsgCli) error {
	pkt, err := msg.Serialized()
	if err != nil {
		return err
	}
	s.sendPktToServer(fmt.Sprint(msg.ProtocolId(), pkt))
	return nil
}

func (s *gameSession) sendMsgToClient(msg d1proto.MsgSvr) error {
	pkt, err := msg.Serialized()
	if err != nil {
		return err
	}
	s.sendPktToClient(fmt.Sprint(msg.ProtocolId(), pkt))
	return nil
}

func (s *gameSession) sendPktToServer(pkt string) {
	id, _ := d1proto.MsgCliIdByPkt(pkt)
	name, _ := d1proto.MsgCliNameByID(id)
	zap.L().Info("sent packet to game server",
		zap.String("server_address", s.serverConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)
	fmt.Fprint(s.serverConn, pkt+"\n\x00")
}

func (s *gameSession) sendPktToClient(pkt string) {
	id, _ := d1proto.MsgSvrIdByPkt(pkt)
	name, _ := d1proto.MsgSvrNameByID(id)
	zap.L().Info("sent packet to game client",
		zap.String("client_address", s.clientConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)
	fmt.Fprint(s.clientConn, pkt+"\x00")
}
