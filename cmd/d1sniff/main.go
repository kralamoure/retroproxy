package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime/trace"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/kralamoure/d1sniff/login"
)

func main() {
	os.Exit(run())
}

var (
	debug           bool
	loginServerAddr string
	loginProxyPort  string
	gameProxyPort   string
	talkToEveryNPC  bool
)

func run() int {
	var wg sync.WaitGroup
	defer wg.Wait()

	loadVars()

	if debug {
		traceFile, err := os.Create("trace.out")
		if err != nil {
			log.Println(err)
			return 1
		}
		defer traceFile.Close()
		err = trace.Start(traceFile)
		if err != nil {
			log.Println(err)
			return 1
		}
		defer trace.Stop()
	}

	var logger *zap.Logger
	if debug {
		tmp, err := zap.NewDevelopment()
		if err != nil {
			log.Println(err)
			return 1
		}
		logger = tmp
	} else {
		tmp, err := zap.NewProduction()
		if err != nil {
			log.Println(err)
			return 1
		}
		logger = tmp
	}
	undoLogger := zap.ReplaceGlobals(logger)
	defer undoLogger()
	defer zap.L().Sync()
	defer zap.S().Sync()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	errCh := make(chan error)

	loginAddr, err := net.ResolveTCPAddr("tcp4", net.JoinHostPort("127.0.0.1", loginProxyPort))
	if err != nil {
		zap.L().Error(err.Error())
		return 1
	}
	loginLn, err := net.ListenTCP("tcp4", loginAddr)
	if err != nil {
		zap.L().Error(err.Error())
		return 1
	}
	defer loginLn.Close()
	loginPx, err := login.NewProxy(loginLn, loginServerAddr, net.JoinHostPort("127.0.0.1", gameProxyPort))
	if err != nil {
		return 1
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := loginPx.Serve(ctx)
		if err != nil {
			select {
			case errCh <- fmt.Errorf("error while serving login proxy: %w", err):
			case <-ctx.Done():
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		var proxy gameProxy
		err := proxy.start(ctx)
		if err != nil {
			select {
			case errCh <- fmt.Errorf("error while serving game proxy: %w", err):
			case <-ctx.Done():
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		deleteOldTicketsLoop(ctx, 10*time.Second)
	}()

	select {
	case sig := <-sigCh:
		zap.L().Info("received signal",
			zap.String("signal", sig.String()),
		)
	case err := <-errCh:
		zap.L().Error(err.Error())
		return 1
	case <-ctx.Done():
	}
	return 0
}

func deleteOldTicketsLoop(ctx context.Context, maxDur time.Duration) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			deleteOldTickets(maxDur)
		case <-ctx.Done():
			return
		}
	}
}

func loadVars() {
	flag.BoolVar(&debug, "d", false, "Enable debug mode")
	flag.StringVar(&loginServerAddr, "a", "34.251.172.139:443", "Dofus login server address")
	flag.StringVar(&loginProxyPort, "lp", "5555", "Dofus login proxy port")
	flag.StringVar(&gameProxyPort, "gp", "5556", "Dofus game proxy port")
	flag.BoolVar(&talkToEveryNPC, "npc", true, "Automatically talk to every NPC")
	flag.Parse()
}
