package login

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/gofrs/uuid"
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
	serverIdCh chan int
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
	id, ok := d1proto.MsgSvrIdByPkt(pkt)
	name, _ := d1proto.MsgSvrNameByID(id)
	zap.L().Info("login: received packet from server",
		zap.String("server_address", s.serverConn.RemoteAddr().String()),
		zap.String("client_address", s.clientConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)
	if ok {
		extra := strings.TrimPrefix(pkt, string(id))
		switch id {
		case d1proto.AccountSelectServerError:
			select {
			case <-s.serverIdCh:
			case <-ctx.Done():
				return ctx.Err()
			}
		case d1proto.AccountSelectServerSuccess:
			var serverId int
			select {
			case serverId = <-s.serverIdCh:
			case <-ctx.Done():
				return ctx.Err()
			}

			msg := &msgsvr.AccountSelectServerSuccess{}
			err := msg.Deserialize(extra)
			if err != nil {
				return err
			}

			id, err := uuid.NewV4()
			if err != nil {
				return err
			}

			d1sniff.SetTicket(id.String(), d1sniff.Ticket{
				Host:     msg.Host,
				Port:     msg.Port,
				Original: msg.Ticket,
				ServerId: serverId,
				IssuedAt: time.Now(),
			})

			msgOut := &msgsvr.AccountSelectServerPlainSuccess{
				Host:   s.proxy.gameHost,
				Port:   s.proxy.gamePort,
				Ticket: id.String(),
			}
			err = s.sendMsgToClient(msgOut)
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
	zap.L().Info("login: received packet from client",
		zap.String("client_address", s.clientConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)

	s.sendPktToServer(pkt)

	if ok {
		extra := strings.TrimPrefix(pkt, string(id))
		switch id {
		case d1proto.AccountSetServer:
			msg := &msgcli.AccountSetServer{}
			err := msg.Deserialize(extra)
			if err != nil {
				return err
			}

			select {
			case s.serverIdCh <- msg.Id:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
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
	zap.L().Info("login: sent packet to server",
		zap.String("server_address", s.serverConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)
	fmt.Fprint(s.serverConn, pkt+"\n\x00")
}

func (s *session) sendPktToClient(pkt string) {
	id, _ := d1proto.MsgSvrIdByPkt(pkt)
	name, _ := d1proto.MsgSvrNameByID(id)
	zap.L().Info("login: sent packet to client",
		zap.String("client_address", s.clientConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)
	fmt.Fprint(s.clientConn, pkt+"\x00")
}
