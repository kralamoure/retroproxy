package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
)

func main() {
	os.Exit(run())
}

var (
	logger             *zap.SugaredLogger
	development        bool
	loginServerAddress string
	loginProxyPort     string
	gameProxyPort      string
	talkToEveryNPC     bool
)

func run() (exitCode int) {
	loadVars()

	if err := loadLogger(); err != nil {
		log.Printf("could not load logger: %s", err)
		return 1
	}
	defer logger.Sync()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Infow("received signal",
			"signal", sig.String(),
		)
		signal.Stop(sigCh)
		cancel()
	}()

	wg := sync.WaitGroup{}

	errCh := make(chan error)

	wg.Add(1)
	go func() {
		err := proxyLogin(ctx)
		wg.Done()
		if err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("error while proxying login server: %w", err)
		}
	}()

	wg.Add(1)
	go func() {
		err := proxyGame(ctx)
		wg.Done()
		if err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("error while proxying game server: %w", err)
		}
	}()

	go func() {
		for {
			wg.Add(1)
			deleteOldTickets(10 * time.Second)
			wg.Done()
			time.Sleep(1 * time.Second)
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		logger.Error(err)
		exitCode = 1
		cancel()
	}
	wg.Wait()
	return exitCode
}

func loadVars() {
	flag.BoolVar(&development, "d", false, "Enable development mode")
	flag.StringVar(&loginProxyPort, "lp", "5555", "Dofus login proxy port")
	flag.StringVar(&gameProxyPort, "gp", "5556", "Dofus game proxy port")
	flag.StringVar(&loginServerAddress, "a", "34.251.172.139:443", "Dofus login server address")
	flag.BoolVar(&talkToEveryNPC, "npc", true, "Automatically talk to every NPC")
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
