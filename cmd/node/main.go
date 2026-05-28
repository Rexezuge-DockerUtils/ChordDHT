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
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	var tracker chord.TrackerClient
	if cfg.TrackerURL != "" {
		trackerClient, err := client.NewTrackerClient(cfg.TrackerURL, cfg.HTTPTimeout, cfg.SkipTLSVerify)
		if err != nil {
			log.Fatalf("invalid tracker URL: %v", err)
		}
		tracker = trackerClient
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
		node.GracefulLeave()
		_ = server.Shutdown(context.Background())
	}()

	log.Printf("node %s listening on %s as %s", node.Self().NodeID, cfg.ListenAddr, cfg.NodeURI)
	if err := server.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
}
