package login

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"github.com/kralamoure/retroproto"
	"github.com/kralamoure/retroproto/msgcli"
	"github.com/kralamoure/retroproto/msgsvr"
	"go.uber.org/zap"

	"github.com/kralamoure/retroproxy"
)

var errEndOfService = errors.New("end of service")

type session struct {
	proxy      *Proxy
	clientConn *net.TCPConn
	serverConn *net.TCPConn
	serverIdCh chan int

	username string
}

type msgOutCli interface {
	MessageId() (id retroproto.MsgCliId)
	Serialized() (extra string, err error)
}

type msgOutSvr interface {
	MessageId() (id retroproto.MsgSvrId)
	Serialized() (extra string, err error)
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
		if err != nil {
			return err
		}
	}
}

func (s *session) handlePktFromServer(ctx context.Context, pkt string) error {
	id, ok := retroproto.MsgSvrIdByPkt(pkt)
	name, _ := retroproto.MsgSvrNameByID(id)
	s.proxy.logger.Info("received packet from server",
		zap.String("server_address", s.serverConn.RemoteAddr().String()),
		zap.String("client_address", s.clientConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)
	if ok {
		extra := strings.TrimPrefix(pkt, string(id))
		switch id {
		case retroproto.AccountLoginSuccess:
			msg := &msgsvr.AccountLoginSuccess{}
			err := msg.Deserialize(extra)
			if err != nil {
				return err
			}

			if s.proxy.forceAdmin {
				msg.Authorized = true
			}

			return s.sendMsgToClient(msg)
		case retroproto.AccountSelectServerError:
			select {
			case <-s.serverIdCh:
			case <-ctx.Done():
				return ctx.Err()
			}
		case retroproto.AccountSelectServerSuccess, retroproto.AccountSelectServerPlainSuccess:
			var serverId int
			select {
			case serverId = <-s.serverIdCh:
			case <-ctx.Done():
				return ctx.Err()
			}

			t := retroproxy.Ticket{ServerId: serverId}

			if id == retroproto.AccountSelectServerSuccess {
				msg := &msgsvr.AccountSelectServerSuccess{}
				err := msg.Deserialize(extra)
				if err != nil {
					return err
				}

				t.Host = msg.Host
				t.Original = msg.Ticket

				if msg.Port == "" {
					t.Port = "443"
				} else {
					t.Port = msg.Port
				}
			} else {
				msg := &msgsvr.AccountSelectServerPlainSuccess{}
				err := msg.Deserialize(extra)
				if err != nil {
					return err
				}

				t.Host = msg.Host
				t.Original = msg.Ticket

				if msg.Port == "" {
					t.Port = "443"
				} else {
					t.Port = msg.Port
				}
			}

			id, err := uuid.NewV4()
			if err != nil {
				return err
			}

			t.IssuedAt = time.Now()
			s.proxy.storer.SetTicket(id.String(), t)

			msg := &msgsvr.AccountSelectServerPlainSuccess{
				Host:   s.proxy.gameHost,
				Port:   s.proxy.gamePort,
				Ticket: id.String(),
			}
			err = s.sendMsgToClient(msg)
			if err != nil {
				return err
			}
			return errEndOfService
		}
	}

	s.sendPktToClient(pkt)
	return nil
}

func (s *session) handlePktFromClient(ctx context.Context, pkt string) error {
	id, ok := retroproto.MsgCliIdByPkt(pkt)
	name, _ := retroproto.MsgCliNameByID(id)
	s.proxy.logger.Info("received packet from client",
		zap.String("client_address", s.clientConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)

	if ok {
		extra := strings.TrimPrefix(pkt, string(id))
		switch id {
		case retroproto.AccountCredential:
			msg := &msgcli.AccountCredential{}
			err := msg.Deserialize(extra)
			if err != nil {
				return err
			}
			s.username = msg.Username
		case retroproto.AccountSetServer:
			s.sendPktToServer(pkt)

			msg := &msgcli.AccountSetServer{}
			err := msg.Deserialize(extra)
			if err != nil {
				return err
			}

			select {
			case s.serverIdCh <- msg.Id:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		case retroproto.AccountConfiguredPort:
			return s.sendMsgToServer(msgcli.AccountConfiguredPort{Port: s.proxy.cache.serverPort})
		case retroproto.AccountSendIdentity:
			id, err := s.identity(ctx)
			if err != nil {
				return err
			}
			return s.sendMsgToServer(msgcli.AccountSendIdentity{Id: id})
		}
	}

	s.sendPktToServer(pkt)

	return nil
}

func (s *session) identity(ctx context.Context) (string, error) {
	v, err := uuid.NewV4()
	if err != nil {
		return "", err
	}

	s.proxy.mu.Lock()
	defer s.proxy.mu.Unlock()

	id, ok := s.proxy.cache.uuidByUsername[s.username]
	if !ok {
		id = v.String()
		s.proxy.cache.uuidByUsername[s.username] = id
	}

	return id, nil
}

func (s *session) sendMsgToServer(msg msgOutCli) error {
	pkt, err := msg.Serialized()
	if err != nil {
		return err
	}
	s.sendPktToServer(fmt.Sprint(msg.MessageId(), pkt))
	return nil
}

func (s *session) sendMsgToClient(msg msgOutSvr) error {
	pkt, err := msg.Serialized()
	if err != nil {
		return err
	}
	s.sendPktToClient(fmt.Sprint(msg.MessageId(), pkt))
	return nil
}

func (s *session) sendPktToServer(pkt string) {
	id, _ := retroproto.MsgCliIdByPkt(pkt)
	name, _ := retroproto.MsgCliNameByID(id)
	s.proxy.logger.Info("sent packet to server",
		zap.String("server_address", s.serverConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)
	fmt.Fprint(s.serverConn, pkt+"\n\x00")
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
