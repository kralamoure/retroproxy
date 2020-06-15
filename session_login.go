package main

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
)

type loginSession struct {
	clientConn net.Conn
	serverConn net.Conn
	serverId   chan int
}

func (s *loginSession) receivePktsFromServer(ctx context.Context) error {
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

func (s *loginSession) receivePktsFromClient(ctx context.Context) error {
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

func (s *loginSession) handlePktFromServer(ctx context.Context, pkt string) error {
	id, ok := d1proto.MsgSvrIdByPkt(pkt)
	name, _ := d1proto.MsgSvrNameByID(id)
	logger.Infow("received packet from login server",
		"server_address", s.serverConn.RemoteAddr().String(),
		"client_address", s.clientConn.RemoteAddr().String(),
		"message_name", name,
		"packet", pkt,
	)
	if ok {
		extra := strings.TrimPrefix(pkt, string(id))
		switch id {
		case d1proto.AccountSelectServerError:
			select {
			case <-s.serverId:
			case <-ctx.Done():
				return ctx.Err()
			}
		case d1proto.AccountSelectServerSuccess:
			var serverId int
			select {
			case serverId = <-s.serverId:
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

			setTicket(id.String(), ticket{
				host:             msg.Host,
				port:             msg.Port,
				originalTicketId: msg.Ticket,
				serverId:         serverId,
				issuedAt:         time.Now(),
			})

			msgOut := &msgsvr.AccountSelectServerPlainSuccess{
				Host:   "localhost",
				Port:   gameProxyPort,
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

func (s *loginSession) handlePktFromClient(ctx context.Context, pkt string) error {
	id, ok := d1proto.MsgCliIdByPkt(pkt)
	name, _ := d1proto.MsgCliNameByID(id)
	logger.Debugw("received packet from login client",
		"client_address", s.clientConn.RemoteAddr().String(),
		"message_name", name,
		"packet", pkt,
	)
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
			case s.serverId <- msg.Id:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	s.sendPktToServer(pkt)
	return nil
}

func (s *loginSession) sendMsgToServer(msg d1proto.MsgCli) error {
	pkt, err := msg.Serialized()
	if err != nil {
		return err
	}
	s.sendPktToServer(fmt.Sprint(msg.ProtocolId(), pkt))
	return nil
}

func (s *loginSession) sendMsgToClient(msg d1proto.MsgSvr) error {
	pkt, err := msg.Serialized()
	if err != nil {
		return err
	}
	s.sendPktToClient(fmt.Sprint(msg.ProtocolId(), pkt))
	return nil
}

func (s *loginSession) sendPktToServer(pkt string) {
	id, _ := d1proto.MsgCliIdByPkt(pkt)
	name, _ := d1proto.MsgCliNameByID(id)
	logger.Infow("sent packet to login server",
		"server_address", s.serverConn.RemoteAddr().String(),
		"message_name", name,
		"packet", pkt,
	)
	fmt.Fprint(s.serverConn, pkt+"\n\x00")
}

func (s *loginSession) sendPktToClient(pkt string) {
	id, _ := d1proto.MsgSvrIdByPkt(pkt)
	name, _ := d1proto.MsgSvrNameByID(id)
	logger.Infow("sent packet to login client",
		"client_address", s.clientConn.RemoteAddr().String(),
		"message_name", name,
		"packet", pkt,
	)
	fmt.Fprint(s.clientConn, pkt+"\x00")
}
