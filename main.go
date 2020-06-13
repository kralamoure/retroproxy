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
	wg := sync.WaitGroup{}
	defer wg.Wait()

	loadVars()

	if err := loadLogger(); err != nil {
		log.Printf("could not load logger: %s", err)
		return 1
	}
	defer logger.Sync()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	errCh := make(chan error, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := proxyLogin(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			select {
			case errCh <- fmt.Errorf("error while proxying login server: %w", err):
			default:
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := proxyGame(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			select {
			case errCh <- fmt.Errorf("error while proxying game server: %w", err):
			default:
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
		logger.Infow("received signal",
			"signal", sig.String(),
		)
	case err := <-errCh:
		logger.Error(err)
		exitCode = 1
	case <-ctx.Done():
	}
	return
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
