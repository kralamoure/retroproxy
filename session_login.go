package main

import (
	"net"
)

type loginSession struct {
	clientConn net.Conn
	serverConn net.Conn
	serverId   int
}
