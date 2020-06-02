package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"go.uber.org/zap"
)

func main() {
	os.Exit(run())
}

var (
	logger         *zap.SugaredLogger
	development    bool
	loginProxyPort string
	gameProxyPort  string
	address        string
)

type session struct {
	clientConn net.Conn
	serverConn net.Conn
	serverId   int
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
			log.Printf("error while proxying login server: %s", err)
		}
		cancel()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := proxyGame(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("error while proxying game server: %s", err)
		}
		cancel()
	}()

	<-ctx.Done()
	wg.Wait()
	return 0
}

func loadVars() {
	flag.BoolVar(&development, "d", false, "Enable development mode")
	flag.StringVar(&loginProxyPort, "lp", "5555", "Dofus login proxy port")
	flag.StringVar(&gameProxyPort, "gp", "5556", "Dofus game proxy port")
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
