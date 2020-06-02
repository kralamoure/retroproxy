package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"go.uber.org/zap"
)

func main() {
	os.Exit(run())
}

var (
	logger      *zap.SugaredLogger
	development bool
	proxyPort   string
	address     string
)

type session struct {
	clientConn net.Conn
	serverConn net.Conn
}

func run() (exitCode int) {
	loadVars()

	if err := loadLogger(); err != nil {
		log.Printf("could not load logger: %s", err)
		return 1
	}
	defer logger.Sync()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		logger.Debugw("received signal",
			"signal", sig.String(),
		)
		signal.Stop(sigs)
		cancel()
	}()

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := proxyLogin(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("error while connecting to login proxy: %s", err)
		}
		cancel()
	}()

	<-ctx.Done()
	wg.Wait()
	return 0
}

func proxyLogin(ctx context.Context) error {
	ln, err := net.Listen("tcp4", net.JoinHostPort("localhost", proxyPort))
	if err != nil {
		return err
	}
	defer ln.Close()
	logger.Infow("started login proxy",
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
	logger.Debugw("new connection from login client",
		"client_address", conn.RemoteAddr().String(),
	)

	errCh := make(chan error, 1)

	serverConn, err := net.Dial("tcp", address)
	if err != nil {
		return err
	}
	logger.Infow("connected to login server",
		"local_address", serverConn.LocalAddr().String(),
		"server_address", serverConn.RemoteAddr().String(),
		"client_address", conn.RemoteAddr().String(),
	)
	defer serverConn.Close()

	sess := session{
		clientConn: conn,
		serverConn: serverConn,
	}

	serverMsgCh := make(chan string)
	go func() {
		rd := bufio.NewReader(serverConn)
		for {
			msg, err := rd.ReadString('\x00')
			if err != nil {
				errCh <- err
				return
			}
			msg = strings.TrimSuffix(msg, "\x00")
			serverMsgCh <- msg
		}
	}()

	clientMsgCh := make(chan string)
	go func() {
		rd := bufio.NewReader(sess.clientConn)
		for {
			msg, err := rd.ReadString('\x00')
			if err != nil {
				errCh <- err
				return
			}
			msg = strings.TrimSuffix(msg, "\n\x00")
			clientMsgCh <- msg
		}
	}()

	for {
		select {
		case msg := <-clientMsgCh:
			handleMsgFromLoginClient(sess, msg)
		case msg := <-serverMsgCh:
			handleMsgFromLoginServer(sess, msg)
		case err := <-errCh:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func handleMsgFromLoginClient(sess session, msg string) {
	logger.Debugw("received message from login client",
		"client_address", sess.clientConn.RemoteAddr().String(),
		"message", msg,
	)
	sendMsgToLoginServer(sess, msg)
}

func handleMsgFromLoginServer(sess session, msg string) {
	logger.Debugw("received message from login server",
		"server_address", sess.serverConn.RemoteAddr().String(),
		"client_address", sess.clientConn.RemoteAddr().String(),
		"message", msg,
	)
	sendMsgToLoginClient(sess, msg)
}

func sendMsgToLoginClient(sess session, msg string) {
	logger.Debugw("sent message to login client",
		"client_address", sess.clientConn.RemoteAddr().String(),
		"message", msg,
	)
	fmt.Fprint(sess.clientConn, msg+"\x00")
}

func sendMsgToLoginServer(sess session, msg string) {
	logger.Debugw("sent message to login server",
		"server_address", sess.serverConn.RemoteAddr().String(),
		"message", msg,
	)
	fmt.Fprint(sess.serverConn, msg+"\x00")
}

func loadVars() {
	flag.BoolVar(&development, "d", false, "Enable development mode")
	flag.StringVar(&proxyPort, "p", "5555", "Dofus login proxy port")
	flag.StringVar(&address, "a", "34.251.172.139:443", "Dofus login server address")
	flag.Parse()
}

func loadLogger() error {
	if development {
		tmp, err := zap.NewDevelopment()
		if err != nil {
			return err
		}
		logger = tmp.Sugar()
	} else {
		tmp, err := zap.NewProduction()
		if err != nil {
			return err
		}
		logger = tmp.Sugar()
	}
	return nil
}
