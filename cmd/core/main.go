package main

import (
	"context"
	"github.com/thomaslmaat/homelab-monitor/internal/httpapi"
	"github.com/thomaslmaat/homelab-monitor/internal/kubernetes"
	"github.com/thomaslmaat/homelab-monitor/internal/storage"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	path := os.Getenv("DATABASE_PATH")
	if path == "" {
		path = "/data/homelab-monitor.db"
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	store, err := storage.Open(path)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer store.Close()
	kube, err := kubernetes.NewInCluster()
	if err != nil {
		log.Printf("Kubernetes API integration unavailable: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := store.Prune(ctx, 7*24*time.Hour); err != nil {
					log.Printf("prune metrics: %v", err)
				}
			}
		}
	}()
	srv := &http.Server{Addr: addr, Handler: httpapi.New(store, kube).Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("Homelab Monitor Core listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}
