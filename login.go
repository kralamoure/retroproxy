package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
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

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				errCh <- err
				return
			}
			connCh <- conn
		}
	}()

	var loopErr error
	wg := sync.WaitGroup{}
LOOP:
	for {
		select {
		case conn := <-connCh:
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := handleLoginConn(ctx, conn)
				if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
					log.Printf("error while handling login connection: %s", err)
				}
			}()
		case err := <-errCh:
			loopErr = err
			break LOOP
		case <-ctx.Done():
			loopErr = ctx.Err()
			break LOOP
		}
	}
	wg.Wait()
	return loopErr
}

func handleLoginConn(ctx context.Context, conn net.Conn) error {
	defer conn.Close()
	logger.Infow("new connection from login client",
		"client_address", conn.RemoteAddr().String(),
	)

	serverConn, err := net.Dial("tcp", address)
	if err != nil {
		return err
	}
	defer serverConn.Close()
	logger.Infow("connected to login server",
		"local_address", serverConn.LocalAddr().String(),
		"server_address", serverConn.RemoteAddr().String(),
		"client_address", conn.RemoteAddr().String(),
	)

	sess := &session{
		clientConn: conn,
		serverConn: serverConn,
	}

	wg := sync.WaitGroup{}
	defer wg.Wait()

	errCh := make(chan error)

	go func() {
		rd := bufio.NewReader(serverConn)
		for {
			pkt, err := rd.ReadString('\x00')
			if err != nil {
				errCh <- err
				return
			}
			pkt = strings.TrimSuffix(pkt, "\x00")
			if pkt == "" {
				continue
			}
			wg.Add(1)
			err = handlePktFromLoginServer(sess, pkt)
			wg.Done()
			if err != nil {
				errCh <- err
				return
			}
		}
	}()

	go func() {
		rd := bufio.NewReader(sess.clientConn)
		for {
			pkt, err := rd.ReadString('\x00')
			if err != nil {
				errCh <- err
				return
			}
			pkt = strings.TrimSuffix(pkt, "\n\x00")
			if pkt == "" {
				continue
			}
			wg.Add(1)
			err = handlePktFromLoginClient(sess, pkt)
			wg.Done()
			if err != nil {
				errCh <- err
				return
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

func handlePktFromLoginClient(sess *session, pkt string) error {
	id, ok := d1proto.MsgCliIdByPkt(pkt)
	name, _ := d1proto.MsgCliNameByID(id)
	logger.Debugw("received packet from login client",
		"client_address", sess.clientConn.RemoteAddr().String(),
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
			sess.serverId = msg.Id
		}
	}

	sendPktToLoginServer(sess, pkt)
	return nil
}

func handlePktFromLoginServer(sess *session, pkt string) error {
	id, ok := d1proto.MsgSvrIdByPkt(pkt)
	name, _ := d1proto.MsgSvrNameByID(id)
	logger.Infow("received packet from login server",
		"server_address", sess.serverConn.RemoteAddr().String(),
		"client_address", sess.clientConn.RemoteAddr().String(),
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
				serverId:         sess.serverId,
				issuedAt:         time.Now(),
			})

			msgOut := &msgsvr.AccountSelectServerPlainSuccess{
				Host:   "localhost",
				Port:   gameProxyPort,
				Ticket: id.String(),
			}
			err = sendMsgToLoginClient(sess, msgOut)
			if err != nil {
				return err
			}
			return nil
		}
	}

	sendPktToLoginClient(sess, pkt)
	return nil
}

func sendMsgToLoginClient(sess *session, msg d1proto.MsgSvr) error {
	pkt, err := msg.Serialized()
	if err != nil {
		return err
	}
	sendPktToLoginClient(sess, fmt.Sprint(msg.ProtocolId(), pkt))
	return nil
}

func sendPktToLoginClient(sess *session, pkt string) {
	id, _ := d1proto.MsgSvrIdByPkt(pkt)
	name, _ := d1proto.MsgSvrNameByID(id)
	logger.Infow("sent packet to login client",
		"client_address", sess.clientConn.RemoteAddr().String(),
		"message_name", name,
		"packet", pkt,
	)
	fmt.Fprint(sess.clientConn, pkt+"\x00")
}

func sendPktToLoginServer(sess *session, pkt string) {
	id, _ := d1proto.MsgCliIdByPkt(pkt)
	name, _ := d1proto.MsgCliNameByID(id)
	logger.Infow("sent packet to login server",
		"server_address", sess.serverConn.RemoteAddr().String(),
		"message_name", name,
		"packet", pkt,
	)
	fmt.Fprint(sess.serverConn, pkt+"\n\x00")
}
