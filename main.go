package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/trace"
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
	debug              bool
	loginServerAddress string
	loginProxyPort     string
	gameProxyPort      string
	talkToEveryNPC     bool
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

	errCh := make(chan error)

	wg.Add(1)
	go func() {
		defer wg.Done()
		var proxy loginProxy
		err := proxy.start(ctx)
		if err != nil {
			select {
			case errCh <- fmt.Errorf("error while proxying login server: %w", err):
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
			case errCh <- fmt.Errorf("error while proxying game server: %w", err):
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
		logger.Infow("received signal",
			"signal", sig.String(),
		)
	case err := <-errCh:
		logger.Error(err)
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

func loadLogger() error {
	if debug {
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

func loadVars() {
	flag.BoolVar(&debug, "d", false, "Enable debug mode")
	flag.StringVar(&loginServerAddress, "a", "34.251.172.139:443", "Dofus login server address")
	flag.StringVar(&loginProxyPort, "lp", "5555", "Dofus login proxy port")
	flag.StringVar(&gameProxyPort, "gp", "5556", "Dofus game proxy port")
	flag.BoolVar(&talkToEveryNPC, "npc", true, "Automatically talk to every NPC")
	flag.Parse()
}
