package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"

	"gitlab.com/dofuspro/d1proto"
	"gitlab.com/dofuspro/d1proto/msgcli"
	"gitlab.com/dofuspro/d1proto/msgsvr"
)

var errMalformedPacket = errors.New("malformed packet")

func proxyGame(ctx context.Context) error {
	ln, err := net.Listen("tcp4", net.JoinHostPort("localhost", gameProxyPort))
	if err != nil {
		return err
	}
	defer ln.Close()
	logger.Infow("started game proxy",
		"address", ln.Addr().String(),
	)

	errCh := make(chan error, 1)
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
				err := handleGameConn(ctx, conn)
				if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
					log.Printf("error while handling game connection: %s", err)
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

func handleGameConn(ctx context.Context, conn net.Conn) error {
	defer conn.Close()
	logger.Debugw("new connection from game client",
		"client_address", conn.RemoteAddr().String(),
	)

	sess := &session{
		clientConn:      conn,
		gameServerCh:    make(chan msgsvr.AccountSelectServerSuccess, 1),
		gameServerPktCh: make(chan string),
	}

	errCh := make(chan error, 1)

	clientPktCh := make(chan string)
	go func() {
		rd := bufio.NewReader(conn)
		for {
			pkt, err := rd.ReadString('\x00')
			if err != nil {
				errCh <- err
				return
			}
			pkt = strings.TrimSuffix(pkt, "\n\x00")
			clientPktCh <- pkt
		}
	}()
	sendPktToGameClient(sess, string(d1proto.AksHelloGame))

	wg := sync.WaitGroup{}

	var loopErr error
LOOP:
	for {
		select {
		case pkt := <-clientPktCh:
			err := handlePktFromGameClient(sess, pkt)
			if err != nil {
				loopErr = err
				break LOOP
			}
		case pkt := <-sess.gameServerPktCh:
			err := handlePktFromGameServer(sess, pkt)
			if err != nil {
				loopErr = err
				break LOOP
			}
		case msg := <-sess.gameServerCh:
			sess.gameServerTicket = msg.Ticket
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := connectToGameServer(ctx, sess, net.JoinHostPort(msg.Host, msg.Port))
				if err != nil {
					errCh <- err
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

func connectToGameServer(ctx context.Context, sess *session, address string) error {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	logger.Infow("connected to game server",
		"local_address", conn.LocalAddr().String(),
		"server_address", conn.RemoteAddr().String(),
		"client_address", sess.clientConn.RemoteAddr().String(),
	)
	sess.serverConn = conn

	errCh := make(chan error, 1)

	rd := bufio.NewReader(conn)
	go func() {
		for {
			pkt, err := rd.ReadString('\x00')
			if err != nil {
				errCh <- err
				return
			}
			pkt = strings.TrimSuffix(pkt, "\x00")
			sess.gameServerPktCh <- pkt
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func handlePktFromGameClient(sess *session, pkt string) error {
	logger.Debugw("received packet from game client",
		"client_address", sess.clientConn.RemoteAddr().String(),
		"packet", pkt,
	)

	id, ok := d1proto.MsgCliIdByPkt(pkt)
	if ok {
		extra := strings.TrimPrefix(pkt, string(id))
		switch id {
		case d1proto.AccountSendTicket:
			if sess.receivedFirstGamePkt {
				break
			}
			sess.receivedFirstGamePkt = true

			sli := strings.SplitN(extra, "|", 4)
			if len(sli) < 4 {
				return errMalformedPacket
			}

			serverId, err := strconv.Atoi(sli[0])
			if err != nil {
				return err
			}
			sess.serverId = serverId

			msg := &msgsvr.AccountSelectServerSuccess{
				Host:   sli[1],
				Port:   sli[2],
				Ticket: sli[3],
			}
			sess.gameServerCh <- *msg
			return nil
		}
	}

	sendPktToGameServer(sess, pkt)
	return nil
}

func handlePktFromGameServer(sess *session, pkt string) error {
	logger.Debugw("received packet from game server",
		"server_address", sess.serverConn.RemoteAddr().String(),
		"client_address", sess.clientConn.RemoteAddr().String(),
		"packet", pkt,
	)

	id, ok := d1proto.MsgSvrIdByPkt(pkt)
	if ok {
		switch id {
		case d1proto.AksHelloGame:
			err := sendMsgToGameServer(sess, &msgcli.AccountSendTicket{Id: sess.gameServerTicket})
			if err != nil {
				return err
			}
			return nil
		}
	}

	sendPktToGameClient(sess, pkt)
	return nil
}

func sendMsgToGameServer(sess *session, msg d1proto.MsgCli) error {
	pkt, err := msg.Serialized()
	if err != nil {
		return err
	}
	sendPktToGameServer(sess, fmt.Sprint(msg.ProtocolId(), pkt))
	return nil
}

func sendPktToGameClient(sess *session, pkt string) {
	logger.Debugw("sent packet to game client",
		"client_address", sess.clientConn.RemoteAddr().String(),
		"packet", pkt,
	)
	fmt.Fprint(sess.clientConn, pkt+"\x00")
}

func sendPktToGameServer(sess *session, pkt string) {
	logger.Debugw("sent packet to game server",
		"server_address", sess.serverConn.RemoteAddr().String(),
		"packet", pkt,
	)
	fmt.Fprint(sess.serverConn, pkt+"\n\x00")
}
