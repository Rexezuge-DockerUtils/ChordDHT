package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"chorddht/internal/chord"
	"chorddht/internal/client"
	"chorddht/internal/config"
	"chorddht/internal/httpapi"
	"chorddht/internal/logging"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}
	if err := logging.SetLevel(cfg.LogLevel); err != nil {
		log.Fatalf("invalid log level: %v", err)
	}
	logging.Infof("starting node uri=%s listen=%s tracker_configured=%t manual_seeds=%d log_level=%s", cfg.NodeURI, cfg.ListenAddr, cfg.TrackerURL != "", len(cfg.ManualSeeds), cfg.LogLevel)
	if cfg.SkipTLSVerify {
		logging.Warnf("outbound TLS certificate verification is disabled")
	}

	var tracker chord.TrackerClient
	if cfg.TrackerURL != "" {
		logging.Infof("using tracker url=%s", cfg.TrackerURL)
		trackerClient, err := client.NewTrackerClient(cfg.TrackerURL, cfg.HTTPTimeout, cfg.SkipTLSVerify)
		if err != nil {
			log.Fatalf("invalid tracker URL: %v", err)
		}
		tracker = trackerClient
	} else {
		logging.Infof("tracker disabled")
	}

	peerClient := client.NewChordClient(cfg.HTTPTimeout, cfg.SkipTLSVerify)
	node, err := chord.NewNode(cfg.NodeURI, cfg.ChordOptions(), peerClient, tracker)
	if err != nil {
		log.Fatalf("failed to initialize node: %v", err)
	}

	manualSeeds := make([]chord.NodeInfo, 0, len(cfg.ManualSeeds))
	for _, seedURI := range cfg.ManualSeeds {
		seed, err := chord.NewNodeInfoFromURI(seedURI)
		if err != nil {
			log.Fatalf("invalid seed URI %q: %v", seedURI, err)
		}
		manualSeeds = append(manualSeeds, seed)
	}

	if err := node.JoinNetwork(manualSeeds); err != nil {
		log.Fatalf("join failed: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go node.RunMaintenance(ctx)

	server := &http.Server{Addr: cfg.ListenAddr, Handler: httpapi.NewServer(node).Handler()}
	go func() {
		<-ctx.Done()
		logging.Infof("shutdown started")
		node.GracefulLeave()
		if err := server.Shutdown(context.Background()); err != nil {
			logging.Warnf("server shutdown failed: %v", err)
		}
		logging.Infof("shutdown completed")
	}()

	logging.Infof("node %s listening on %s as %s", node.Self().NodeID, cfg.ListenAddr, cfg.NodeURI)
	if err := server.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
}
