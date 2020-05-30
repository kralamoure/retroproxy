package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
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
	address     string
	version     string
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
		if err := connectToLogin(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("error while connecting to login server: %s", err)
		}
		cancel()
	}()

	<-ctx.Done()
	wg.Wait()
	return 0
}

func connectToLogin(ctx context.Context) error {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return err
	}
	logger.Infow("connected to login server",
		"local_address", conn.LocalAddr().String(),
		"remote_address", conn.RemoteAddr().String(),
	)
	defer conn.Close()

	errs := make(chan error, 1)
	msgs := make(chan string)

	go func() {
		r := bufio.NewReader(conn)
		for {
			msg, err := r.ReadString('\x00')
			if err != nil {
				errs <- err
				break
			}
			msg = strings.TrimSuffix(msg, "\x00")
			msgs <- msg
		}
	}()

	var err2 error
LOOP:
	for {
		select {
		case <-ctx.Done():
			err2 = ctx.Err()
			break LOOP
		case err := <-errs:
			err2 = err
			break LOOP
		case msg := <-msgs:
			logger.Debugw("received message",
				"message", msg,
			)
		}
	}
	return err2
}

func loadVars() {
	flag.BoolVar(&development, "d", false, "Enable development mode")
	flag.StringVar(&address, "a", "34.251.172.139:443", "Dofus login server address")
	flag.StringVar(&version, "v", "1.32.1", "Dofus version")
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
