package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/thomaslmaat/homelab-monitor/internal/collector"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	endpoint := os.Getenv("CORE_URL")
	if endpoint == "" {
		endpoint = "http://homelab-monitor.homelab-monitor.svc.cluster.local:8080"
	}
	endpoint += "/api/v1/metrics"
	interval := 15 * time.Second
	if v := os.Getenv("COLLECTION_INTERVAL"); v != "" {
		if d, e := time.ParseDuration(v); e == nil && d >= 5*time.Second {
			interval = d
		}
	}
	client := &http.Client{Timeout: 5 * time.Second}
	c := collector.NewLinux()
	log.Printf("Homelab Monitor Agent started; node=%s interval=%s", os.Getenv("NODE_NAME"), interval)
	for {
		m, err := c.Collect()
		if err != nil {
			log.Printf("collect metrics: %v", err)
		} else if err = send(client, endpoint, m); err != nil {
			log.Printf("send metrics: %v", err)
		}
		time.Sleep(interval)
	}
}
func send(client *http.Client, endpoint string, m any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := os.Getenv("HOMELAB_MONITOR_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("core returned HTTP %d", resp.StatusCode)
	}
	return nil
}
