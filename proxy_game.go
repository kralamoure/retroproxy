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

	"github.com/kralamoure/d1proto"
	"github.com/kralamoure/d1proto/enum"
	"github.com/kralamoure/d1proto/msgcli"
	"github.com/kralamoure/d1proto/msgsvr"
)

type gameStatus int

const (
	gameStatusIdle gameStatus = iota
	gameStatusWaitingForDialogCreateResponse
	gameStatusWaitingForDialogQuestion
	gameStatusWaitingForDialogLeave
)

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
	logger.Infow("new connection from game client",
		"client_address", conn.RemoteAddr().String(),
	)

	sess := &gameSession{
		clientConn:         conn,
		gameServerCh:       make(chan msgsvr.AccountSelectServerSuccess, 1),
		gameServerPktCh:    make(chan string, 64),
		gameServerMsgOutCh: make(chan d1proto.MsgCli, 64),
	}

	errCh := make(chan error, 1)

	clientPktCh := make(chan string, 64)
	go func() {
		rd := bufio.NewReader(conn)
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
			if sess.gameStatus != gameStatusIdle || sess.dialogsLeft != 0 {
				clientPktCh <- pkt
				continue
			}
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
		case msg := <-sess.gameServerMsgOutCh:
			id := msg.ProtocolId()
			switch id {
			case d1proto.DialogCreate:
				if sess.gameStatus != gameStatusIdle {
					sess.gameServerMsgOutCh <- msg
					continue
				}
				sess.gameStatus = gameStatusWaitingForDialogCreateResponse
			}
			err := sendMsgToGameServer(sess, msg)
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

func connectToGameServer(ctx context.Context, sess *gameSession, address string) error {
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
			if pkt == "" {
				continue
			}
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

func handlePktFromGameClient(sess *gameSession, pkt string) error {
	id, ok := d1proto.MsgCliIdByPkt(pkt)
	name, _ := d1proto.MsgCliNameByID(id)
	logger.Debugw("received packet from game client",
		"client_address", sess.clientConn.RemoteAddr().String(),
		"message_name", name,
		"packet", pkt,
	)
	if ok {
		extra := strings.TrimPrefix(pkt, string(id))
		switch id {
		case d1proto.AccountSendTicket:
			if sess.receivedFirstGamePkt {
				break
			}
			sess.receivedFirstGamePkt = true

			t, ok := useTicket(extra)
			if !ok {
				return errors.New("ticket not found")
			}

			sess.serverId = t.serverId

			msg := &msgsvr.AccountSelectServerSuccess{
				Host:   t.host,
				Port:   t.port,
				Ticket: t.originalTicketId,
			}
			sess.gameServerCh <- *msg
			return nil
		}
	}

	sendPktToGameServer(sess, pkt)
	return nil
}

func handlePktFromGameServer(sess *gameSession, pkt string) error {
	id, ok := d1proto.MsgSvrIdByPkt(pkt)
	name, _ := d1proto.MsgSvrNameByID(id)
	logger.Infow("received packet from game server",
		"server_address", sess.serverConn.RemoteAddr().String(),
		"client_address", sess.clientConn.RemoteAddr().String(),
		"message_name", name,
		"packet", pkt,
	)
	if ok {
		extra := strings.TrimPrefix(pkt, string(id))
		switch id {
		case d1proto.AksHelloGame:
			err := sendMsgToGameServer(sess, &msgcli.AccountSendTicket{Id: sess.gameServerTicket})
			if err != nil {
				return err
			}
			return nil
		case d1proto.GameMovement:
			if !talkToEveryNPC {
				break
			}
			msg := &msgsvr.GameMovement{}
			err := msg.Deserialize(extra)
			if err != nil {
				return err
			}
			for _, sprite := range msg.Sprites {
				if sprite.Type != enum.GameMovementSpriteType.NPC {
					continue
				}
				sess.dialogsLeft++
				sess.gameServerMsgOutCh <- &msgcli.DialogCreate{NPCId: sprite.Id}
			}
			err = sendMsgToGameClient(sess, msg)
			if err != nil {
				return err
			}
			return nil
		case d1proto.DialogCreateError:
			if sess.gameStatus == gameStatusWaitingForDialogCreateResponse {
				sess.gameStatus = gameStatusIdle
				sess.dialogsLeft--
				return nil
			}
		case d1proto.DialogCreateSuccess:
			if sess.gameStatus == gameStatusWaitingForDialogCreateResponse {
				sess.gameStatus = gameStatusWaitingForDialogQuestion
				return nil
			}
		case d1proto.DialogQuestion:
			if sess.gameStatus == gameStatusWaitingForDialogQuestion {
				sess.gameServerMsgOutCh <- &msgcli.DialogRequestLeave{}
				sess.gameStatus = gameStatusWaitingForDialogLeave
				return nil
			}
		case d1proto.DialogLeave:
			if sess.gameStatus == gameStatusWaitingForDialogLeave {
				sess.gameStatus = gameStatusIdle
				sess.dialogsLeft--
				return nil
			}
		}
	}

	sendPktToGameClient(sess, pkt)
	return nil
}

func sendMsgToGameClient(sess *gameSession, msg d1proto.MsgSvr) error {
	pkt, err := msg.Serialized()
	if err != nil {
		return err
	}
	sendPktToGameClient(sess, fmt.Sprint(msg.ProtocolId(), pkt))
	return nil
}

func sendMsgToGameServer(sess *gameSession, msg d1proto.MsgCli) error {
	pkt, err := msg.Serialized()
	if err != nil {
		return err
	}
	sendPktToGameServer(sess, fmt.Sprint(msg.ProtocolId(), pkt))
	return nil
}

func sendPktToGameClient(sess *gameSession, pkt string) {
	id, _ := d1proto.MsgSvrIdByPkt(pkt)
	name, _ := d1proto.MsgSvrNameByID(id)
	logger.Infow("sent packet to game client",
		"client_address", sess.clientConn.RemoteAddr().String(),
		"message_name", name,
		"packet", pkt,
	)
	fmt.Fprint(sess.clientConn, pkt+"\x00")
}

func sendPktToGameServer(sess *gameSession, pkt string) {
	id, _ := d1proto.MsgCliIdByPkt(pkt)
	name, _ := d1proto.MsgCliNameByID(id)
	logger.Infow("sent packet to game server",
		"server_address", sess.serverConn.RemoteAddr().String(),
		"message_name", name,
		"packet", pkt,
	)
	fmt.Fprint(sess.serverConn, pkt+"\n\x00")
}
