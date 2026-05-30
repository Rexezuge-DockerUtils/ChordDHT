package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"chorddht/internal/auth"
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
	logging.Infof("starting node uri=%s listen=%s tracker_configured=%t manual_seeds=%d log_level=%s auth=%t region=%s",
		cfg.NodeURI, cfg.ListenAddr, cfg.TrackerURL != "", len(cfg.ManualSeeds), cfg.LogLevel, cfg.Auth.Enabled, cfg.NodeRegion)
	if cfg.SkipTLSVerify {
		logging.Warnf("outbound TLS certificate verification is disabled")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Auth setup ---
	var signer *auth.RequestSigner
	var verifier *auth.RequestVerifier
	chordOpts := cfg.ChordOptions()

	if cfg.Auth.Enabled {
		caPubKeyBytes, err := base64.RawURLEncoding.DecodeString(cfg.Auth.CAPublicKeyBase64)
		if err != nil || len(caPubKeyBytes) != 32 {
			log.Fatalf("invalid auth.ca-public-key-base64: must be base64url-encoded 32-byte Ed25519 key")
		}
		caPubKey := ed25519.PublicKey(caPubKeyBytes)

		certData, err := os.ReadFile(cfg.Auth.NodeCertificateFile)
		if err != nil {
			log.Fatalf("read node certificate: %v", err)
		}
		nodeCert, err := auth.ParseCertificate(certData)
		if err != nil {
			log.Fatalf("parse node certificate: %v", err)
		}
		if err := auth.VerifyCertificate(nodeCert, caPubKey, time.Now(), cfg.Auth.TimestampToleranceSecs); err != nil {
			log.Fatalf("node certificate invalid: %v", err)
		}
		if nodeCert.IsExpiringSoon(time.Now(), cfg.Auth.CertExpiryWarnDays) {
			logging.Warnf("node certificate expires soon expires_at=%d", nodeCert.ExpiresAt)
		}

		privKeyRaw, err := os.ReadFile(cfg.Auth.NodePrivateKeyFile)
		if err != nil {
			log.Fatalf("read node private key: %v", err)
		}
		privKeyBytes, err := base64.RawURLEncoding.DecodeString(string(privKeyRaw))
		if err != nil || len(privKeyBytes) != 64 {
			log.Fatalf("invalid node private key: must be base64url-encoded 64-byte Ed25519 private key")
		}
		privKey := ed25519.PrivateKey(privKeyBytes)

		signer = auth.NewRequestSigner(nodeCert.NodeID, privKey, nodeCert)

		nonceCache := auth.NewNonceCache(
			time.Duration(cfg.Auth.NonceCacheTTLSecs)*time.Second,
			cfg.Auth.NonceCacheMaxSize,
		)
		nonceCache.StartCleanup(ctx)

		certCache := auth.NewCertCache(time.Duration(cfg.Auth.CertCacheTTLSecs) * time.Second)

		var initialCRL *auth.CRL
		if cfg.Auth.CRLFile != "" {
			initialCRL, err = auth.LoadCRLFromFile(cfg.Auth.CRLFile, caPubKey)
			if err != nil {
				log.Fatalf("load crl file: %v", err)
			}
			logging.Infof("loaded CRL from file revoked_count=%d", len(initialCRL.RevokedIDs))
		}

		var gracePeriodEnd time.Time
		if cfg.Auth.BootGracePeriodSecs > 0 {
			gracePeriodEnd = time.Now().Add(time.Duration(cfg.Auth.BootGracePeriodSecs) * time.Second)
		}

		verifierCfg := auth.VerifierConfig{
			CAPublicKey:        caPubKey,
			TimestampTolerance: time.Duration(cfg.Auth.TimestampToleranceSecs) * time.Second,
			NonceCache:         nonceCache,
			CertCache:          certCache,
			ToleranceSecs:      cfg.Auth.TimestampToleranceSecs,
			BootGracePeriodEnd: gracePeriodEnd,
		}
		verifier = auth.NewRequestVerifier(verifierCfg, initialCRL)

		// Store cert JSON in ChordOptions so node can attach it to join/notify/register.
		chordOpts.NodeCertificate, _ = json.Marshal(nodeCert)
		chordOpts.NodeCertExpiresAt = signer.CertExpiresAt()

		if cfg.Auth.CRLRefreshFromTracker {
			chordOpts.OnCRLRefresh = func(crlJSON []byte) {
				crl, err := auth.ParseCRL(crlJSON, caPubKey)
				if err != nil {
					logging.Warnf("crl refresh failed: %v", err)
					return
				}
				verifier.SetCRL(crl)
				logging.Debugf("crl updated from tracker version=%d revoked=%d", crl.Version, len(crl.RevokedIDs))
			}
		}
	}

	// --- Tracker ---
	var tracker chord.TrackerClient
	var trackerClient *client.TrackerClient
	if cfg.TrackerURL != "" {
		logging.Infof("using tracker url=%s", cfg.TrackerURL)
		var err error
		trackerClient, err = client.NewTrackerClient(cfg.TrackerURL, cfg.HTTPTimeout, cfg.SkipTLSVerify)
		if err != nil {
			log.Fatalf("invalid tracker URL: %v", err)
		}
		tracker = trackerClient
	} else {
		logging.Infof("tracker disabled")
	}

	peerClient := client.NewChordClient(cfg.HTTPTimeout, cfg.SkipTLSVerify, signer)
	if cfg.NodeRegion == "" && trackerClient != nil {
		if region, err := trackerClient.DetectRegion(); err == nil && region != "" {
			cfg.NodeRegion = region
			chordOpts.Region = region
			logging.Infof("auto-detected node region from tracker region=%s", region)
		}
	}
	peerClient.SetSelfRegion(cfg.NodeRegion)
	peerClient.SetTimeoutConfig(client.TimeoutConfig{
		PingSame:           cfg.TimeoutPingSameRegion,
		PingCross:          cfg.TimeoutPingCrossRegion,
		FindSuccessorSame:  cfg.TimeoutFindSuccessorSame,
		FindSuccessorCross: cfg.TimeoutFindSuccessorCross,
		FixFingersSame:     cfg.TimeoutFixFingersSame,
		FixFingersCross:    cfg.TimeoutFixFingersCross,
		Default:            cfg.HTTPTimeout,
	})
	node, err := chord.NewNode(cfg.NodeURI, chordOpts, peerClient, tracker)
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
	if cfg.NodeRegion == "" {
		peerClient.SetSelfRegion(node.Region())
	}

	go node.RunMaintenance(ctx)

	server := &http.Server{Addr: cfg.ListenAddr, Handler: httpapi.NewServer(node, verifier).Handler()}
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
