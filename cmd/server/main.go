package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"anymanager/internal/app"
	"anymanager/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	application, err := app.New(ctx, cfg)
	if err != nil {
		log.Fatalf("initialize app: %v", err)
	}
	if err := application.Run(ctx); err != nil {
		log.Fatalf("run app: %v", err)
	}
}
