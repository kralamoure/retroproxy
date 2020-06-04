package main

import (
	"net"

	"gitlab.com/dofuspro/d1proto"
	"gitlab.com/dofuspro/d1proto/msgsvr"
)

type session struct {
	clientConn net.Conn
	serverConn net.Conn
	serverId   int

	receivedFirstGamePkt bool
	gameServerCh         chan msgsvr.AccountSelectServerSuccess
	gameServerPktCh      chan string
	gameServerMsgOutCh   chan d1proto.MsgCli
	gameServerTicket     string

	gameStatus  gameStatus
	dialogsLeft int
}
