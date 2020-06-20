package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/trace"
	"sync"
	"syscall"
	"time"

	flag "github.com/spf13/pflag"

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
	loginProxyAddr  string
	gameProxyAddr   string
	talkToEveryNPC  bool
)

var logger *zap.Logger

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
	defer logger.Sync()

	var wg sync.WaitGroup
	defer wg.Wait()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	errCh := make(chan error)

	repo := d1sniff.NewCache(logger.Named("cache"))

	loginPx, err := login.NewProxy(
		loginProxyAddr,
		loginServerAddr,
		gameProxyAddr,
		repo,
		logger.Named("login"),
	)
	if err != nil {
		logger.Error("could not make login proxy", zap.Error(err))
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
		gameProxyAddr,
		repo,
		logger.Named("game"),
	)
	if err != nil {
		logger.Error("could not make game proxy", zap.Error(err))
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
		logger.Info("received signal",
			zap.String("signal", sig.String()),
		)
	case err := <-errCh:
		logger.Error(err.Error())
		return 1
	case <-ctx.Done():
	}
	return 0
}

func loadVars() {
	flag.BoolVarP(&debug, "debug", "d", false, "Enable debug mode")
	flag.StringVarP(&loginServerAddr, "server", "s",
		"co-retro-0d2e31a98f729b76.elb.eu-west-1.amazonaws.com:443", "Dofus login server address")
	flag.StringVarP(&loginProxyAddr, "login", "l", ":5555", "Dofus login proxy address")
	flag.StringVarP(&gameProxyAddr, "game", "g", ":5556", "Dofus game proxy address")
	flag.Parse()
}
