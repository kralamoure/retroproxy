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

	"github.com/kralamoure/d1sniff"
	"github.com/kralamoure/d1sniff/game"
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

	var wg sync.WaitGroup
	defer wg.Wait()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	errCh := make(chan error)

	repo := &d1sniff.Cache{}

	loginPx, err := login.NewProxy(
		net.JoinHostPort("127.0.0.1", loginProxyPort),
		loginServerAddr,
		net.JoinHostPort("127.0.0.1", gameProxyPort),
		repo,
	)
	if err != nil {
		zap.L().Error("could not make login proxy", zap.Error(err))
		return 1
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := loginPx.ListenAndServe(ctx)
		if err != nil {
			select {
			case errCh <- fmt.Errorf("error while serving login proxy: %w", err):
			case <-ctx.Done():
			}
		}
	}()

	gamePx, err := game.NewProxy(
		net.JoinHostPort("127.0.0.1", gameProxyPort),
		repo,
	)
	if err != nil {
		zap.L().Error("could not make game proxy", zap.Error(err))
		return 1
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := gamePx.ListenAndServe(ctx)
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
		d1sniff.DeleteOldTicketsLoop(ctx, repo, 10*time.Second)
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

func loadVars() {
	flag.BoolVar(&debug, "d", false, "Enable debug mode")
	flag.StringVar(&loginServerAddr, "a", "34.251.172.139:443", "Dofus login server address")
	flag.StringVar(&loginProxyPort, "lp", "5555", "Dofus login proxy port")
	flag.StringVar(&gameProxyPort, "gp", "5556", "Dofus game proxy port")
	flag.BoolVar(&talkToEveryNPC, "npc", true, "Automatically talk to every NPC")
	flag.Parse()
}
