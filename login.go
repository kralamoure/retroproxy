package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/uuid"
	"github.com/kralamoure/d1proto"
	"github.com/kralamoure/d1proto/msgcli"
	"github.com/kralamoure/d1proto/msgsvr"
)

func proxyLogin(ctx context.Context) error {
	wg := sync.WaitGroup{}
	defer wg.Wait()

	ln, err := net.Listen("tcp4", net.JoinHostPort("localhost", loginProxyPort))
	if err != nil {
		return err
	}
	defer ln.Close()
	logger.Infow("started login proxy",
		"address", ln.Addr().String(),
	)

	errCh := make(chan error)
	connCh := make(chan net.Conn)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case errCh <- err:
				case <-ctx.Done():
				}
				return
			}
			connCh <- conn
		}
	}()

	for {
		select {
		case conn := <-connCh:
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := handleLoginConn(ctx, conn)
				if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
					logger.Debugf("error while handling login connection: %s", err)
				}
			}()
		case err := <-errCh:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func handleLoginConn(ctx context.Context, conn net.Conn) error {
	wg := sync.WaitGroup{}
	defer wg.Wait()

	defer conn.Close()
	logger.Infow("new connection from login client",
		"client_address", conn.RemoteAddr().String(),
	)

	serverConn, err := net.Dial("tcp4", loginServerAddress)
	if err != nil {
		return err
	}
	defer serverConn.Close()
	logger.Infow("connected to login server",
		"local_address", serverConn.LocalAddr().String(),
		"server_address", serverConn.RemoteAddr().String(),
		"client_address", conn.RemoteAddr().String(),
	)

	sess := &loginSession{
		clientConn: conn,
		serverConn: serverConn,
	}

	errCh := make(chan error)

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := sess.readFromServerLoop()
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
		err := sess.readFromClientLoop()
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

func (s *loginSession) readFromServerLoop() error {
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
		err = s.handlePktFromLoginServer(pkt)
		if err != nil {
			return err
		}
	}
}

func (s *loginSession) readFromClientLoop() error {
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
		err = s.handlePktFromLoginClient(pkt)
		if err != nil {
			return err
		}
	}
}

func (s *loginSession) handlePktFromLoginClient(pkt string) error {
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
			s.serverId = msg.Id
		}
	}

	s.sendPktToLoginServer(pkt)
	return nil
}

func (s *loginSession) handlePktFromLoginServer(pkt string) error {
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
		case d1proto.AccountSelectServerSuccess:
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
				serverId:         s.serverId,
				issuedAt:         time.Now(),
			})

			msgOut := &msgsvr.AccountSelectServerPlainSuccess{
				Host:   "localhost",
				Port:   gameProxyPort,
				Ticket: id.String(),
			}
			err = s.sendMsgToLoginClient(msgOut)
			if err != nil {
				return err
			}
			return nil
		}
	}

	s.sendPktToLoginClient(pkt)
	return nil
}

func (s *loginSession) sendMsgToLoginClient(msg d1proto.MsgSvr) error {
	pkt, err := msg.Serialized()
	if err != nil {
		return err
	}
	s.sendPktToLoginClient(fmt.Sprint(msg.ProtocolId(), pkt))
	return nil
}

func (s *loginSession) sendPktToLoginClient(pkt string) {
	id, _ := d1proto.MsgSvrIdByPkt(pkt)
	name, _ := d1proto.MsgSvrNameByID(id)
	logger.Infow("sent packet to login client",
		"client_address", s.clientConn.RemoteAddr().String(),
		"message_name", name,
		"packet", pkt,
	)
	fmt.Fprint(s.clientConn, pkt+"\x00")
}

func (s *loginSession) sendPktToLoginServer(pkt string) {
	id, _ := d1proto.MsgCliIdByPkt(pkt)
	name, _ := d1proto.MsgCliNameByID(id)
	logger.Infow("sent packet to login server",
		"server_address", s.serverConn.RemoteAddr().String(),
		"message_name", name,
		"packet", pkt,
	)
	fmt.Fprint(s.serverConn, pkt+"\n\x00")
}
