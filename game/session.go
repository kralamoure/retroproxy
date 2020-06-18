package game

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/kralamoure/d1proto"
	"go.uber.org/zap"
)

type session struct {
	proxy      *Proxy
	clientConn *net.TCPConn
	serverConn *net.TCPConn
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
	id, _ := d1proto.MsgSvrIdByPkt(pkt)
	name, _ := d1proto.MsgSvrNameByID(id)
	zap.L().Info("game: received packet from server",
		zap.String("server_address", s.serverConn.RemoteAddr().String()),
		zap.String("client_address", s.clientConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)

	s.sendPktToClient(pkt)
	return nil
}

func (s *session) handlePktFromClient(ctx context.Context, pkt string) error {
	id, _ := d1proto.MsgCliIdByPkt(pkt)
	name, _ := d1proto.MsgCliNameByID(id)
	zap.L().Info("game: received packet from client",
		zap.String("client_address", s.clientConn.RemoteAddr().String()),
		zap.String("message_name", name),
		zap.String("packet", pkt),
	)

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
