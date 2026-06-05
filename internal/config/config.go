package config

import (
	"errors"
	"flag"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"chorddht/internal/chord"
	"chorddht/internal/logging"
)

type AuthConfig struct {
	Enabled                bool
	CAPublicKeyBase64      string
	NodeCertificateFile    string
	NodePrivateKeyFile     string
	TimestampToleranceSecs int
	NonceCacheTTLSecs      int
	NonceCacheMaxSize      int
	CertCacheTTLSecs       int
	CRLFile                string
	CRLRefreshFromTracker  bool
	CertExpiryWarnDays     int
	BootGracePeriodSecs    int
}

type Config struct {
	NodeURI                string
	ListenAddr             string
	TLSCertFile            string
	TLSKeyFile             string
	SkipTLSVerify          bool
	LogLevel               string
	TrackerURL             string
	ManualSeeds            []string
	HTTPTimeout            time.Duration
	MaintenanceInterval    time.Duration
	SuccessorListSize      int
	MaxHops                int
	SuspiciousThreshold    int
	FailedThreshold        int
	TrackerSeedCount       int
	PingLivenessTimeout    time.Duration
	StabilizeAtomicState   bool
	ValidateAfterStabilize bool
	RectifyEndpointAlias   bool
	InvariantAuditInterval time.Duration
	StableBaseMinSize      int
	StableBaseMembers      []string
	Auth                   AuthConfig

	// v4.0 vnode fields
	VNodeCount                 int
	MaxVNodes                  int
	VNodeProofTTL              time.Duration
	VNodeProofRenewBefore      time.Duration
	ClockSkewTolerance         time.Duration
	VNodeGoroutineLimit        int
	VNodeMaintenanceJitter     time.Duration
	SiblingRouteMaxHops        int
	SuccessorListSiblingCap    float64
	VNodeBootstrapPreferExt    bool
	SharedNodeInfoCacheSize    int
	SharedNodeInfoCacheTTL     time.Duration
	SharedRTTCacheTTL          time.Duration
	SharedRouteCacheSize       int
	SharedRouteCacheTTL        time.Duration
	SharedProofVerifyCacheSize int
	TransferTimeout            time.Duration

	// v3.0 fields
	NodeRegion                     string
	PredecessorListSize            int
	FixFingersBatchSizeActive      int
	FixFingersBatchSizeQuiet       int
	RoutingCacheEnabled            bool
	RoutingCacheSize               int
	RoutingCacheTTL                time.Duration
	LatencyWeightID                float64
	LatencyWeightRTT               float64
	LatencyWeightRegion            float64
	ParallelLookupEnabled          bool
	ParallelLookupCandidates       int
	TimeoutPingSameRegion          time.Duration
	TimeoutPingCrossRegion         time.Duration
	TimeoutFindSuccessorSame       time.Duration
	TimeoutFindSuccessorCross      time.Duration
	TimeoutFixFingersSame          time.Duration
	TimeoutFixFingersCross         time.Duration
	LatencyProbeIntervalActive     time.Duration
	LatencyProbeIntervalQuiet      time.Duration
	RTTEWMAAlpha                   float64
	RTTSampleExpiry                time.Duration
	PiggybackEnabled               bool
	StabilizeDebounceThreshold     int
	TopologyChangeWindow           time.Duration
	StabilizeActiveInterval        time.Duration
	StabilizeQuietInterval         time.Duration
	FixFingersActiveInterval       time.Duration
	FixFingersQuietInterval        time.Duration
	CheckPredecessorActiveInterval time.Duration
	CheckPredecessorQuietInterval  time.Duration
}

func Load() (Config, error) {
	var seeds string
	var stableBaseMembers string
	cfg := Config{}
	flag.StringVar(&cfg.NodeURI, "uri", "", "canonical node https:// URI")
	flag.StringVar(&cfg.ListenAddr, "listen", "", "TLS listen address, defaults to URI port")
	flag.StringVar(&cfg.TLSCertFile, "tls-cert", "", "TLS certificate file")
	flag.StringVar(&cfg.TLSKeyFile, "tls-key", "", "TLS private key file")
	flag.BoolVar(&cfg.SkipTLSVerify, "skip-tls-verify", false, "skip outbound TLS certificate verification")
	flag.StringVar(&cfg.LogLevel, "log-level", "info", "log level: debug, info, warn, error")
	flag.StringVar(&cfg.TrackerURL, "tracker-url", "", "optional tracker https:// URI")
	flag.StringVar(&seeds, "seeds", "", "comma-separated manual seed https:// URIs")
	flag.DurationVar(&cfg.HTTPTimeout, "http-timeout", chord.DefaultHTTPTimeout, "HTTP request timeout")
	flag.DurationVar(&cfg.MaintenanceInterval, "maintenance-interval", chord.DefaultMaintenanceInterval, "maintenance interval")
	flag.IntVar(&cfg.SuccessorListSize, "successor-list-size", chord.DefaultSuccessorListSize, "successor list size")
	flag.IntVar(&cfg.MaxHops, "max-hops", chord.DefaultMaxHops, "maximum find_successor hops")
	flag.IntVar(&cfg.SuspiciousThreshold, "suspicious-threshold", chord.DefaultSuspiciousThreshold, "suspicious threshold")
	flag.IntVar(&cfg.FailedThreshold, "failed-threshold", chord.DefaultFailedThreshold, "failed threshold")
	flag.IntVar(&cfg.TrackerSeedCount, "tracker-seed-count", chord.DefaultTrackerSeedCount, "tracker seed count")
	flag.DurationVar(&cfg.PingLivenessTimeout, "ping-liveness-timeout", chord.DefaultPingLivenessTimeout, "liveness ping timeout")
	flag.BoolVar(&cfg.StabilizeAtomicState, "stabilize-atomic-state", true, "use atomic /state RPC during stabilize")
	flag.BoolVar(&cfg.ValidateAfterStabilize, "validate-after-stabilize", true, "validate successor list after stabilize")
	flag.BoolVar(&cfg.RectifyEndpointAlias, "rectify-endpoint-alias", true, "serve /notify as a /rectify compatibility alias")
	flag.DurationVar(&cfg.InvariantAuditInterval, "invariant-audit-interval", chord.DefaultInvariantAuditInterval, "debug invariant audit interval, 0 disables")
	flag.IntVar(&cfg.StableBaseMinSize, "stable-base-min-size", 0, "stable base minimum size, defaults to successor-list-size+1")
	flag.StringVar(&stableBaseMembers, "stable-base-members", "", "comma-separated stable base anchor https:// URIs")
	flag.BoolVar(&cfg.Auth.Enabled, "auth.enabled", false, "enable node identity authentication")
	flag.StringVar(&cfg.Auth.CAPublicKeyBase64, "auth.ca-public-key-base64", "", "CA Ed25519 public key (base64url)")
	flag.StringVar(&cfg.Auth.NodeCertificateFile, "auth.node-certificate-file", "", "node certificate JSON file")
	flag.StringVar(&cfg.Auth.NodePrivateKeyFile, "auth.node-private-key-file", "", "node Ed25519 private key file (base64url)")
	flag.IntVar(&cfg.Auth.TimestampToleranceSecs, "auth.timestamp-tolerance-secs", 300, "request timestamp tolerance (seconds)")
	flag.IntVar(&cfg.Auth.NonceCacheTTLSecs, "auth.nonce-cache-ttl-secs", 600, "nonce cache TTL (seconds)")
	flag.IntVar(&cfg.Auth.NonceCacheMaxSize, "auth.nonce-cache-max-size", 10000, "nonce cache max entries")
	flag.IntVar(&cfg.Auth.CertCacheTTLSecs, "auth.cert-cache-ttl-secs", 3600, "cert cache TTL (seconds)")
	flag.StringVar(&cfg.Auth.CRLFile, "auth.crl-file", "", "CRL JSON file path")
	flag.BoolVar(&cfg.Auth.CRLRefreshFromTracker, "auth.crl-refresh-from-tracker", true, "auto-refresh CRL from tracker")
	flag.IntVar(&cfg.Auth.CertExpiryWarnDays, "auth.cert-expiry-warn-days", 30, "days before cert expiry to warn")
	flag.IntVar(&cfg.Auth.BootGracePeriodSecs, "auth.boot-grace-period-secs", 0, "seconds after startup before auth is enforced")

	// v4.0 flags
	flag.IntVar(&cfg.VNodeCount, "vnode-count", 0, "number of virtual nodes to spawn (0 = pure anchor mode)")
	flag.IntVar(&cfg.MaxVNodes, "max-vnodes", 8, "maximum vnodes allowed per anchor")
	flag.DurationVar(&cfg.VNodeProofTTL, "vnodeproof-ttl", 86400*time.Second, "VNodeProof validity period")
	flag.DurationVar(&cfg.VNodeProofRenewBefore, "vnodeproof-renew-before", 3600*time.Second, "seconds before expiry to renew VNodeProof")
	flag.DurationVar(&cfg.ClockSkewTolerance, "clock-skew-tolerance", 30*time.Second, "VNodeProof expiry clock skew tolerance")
	flag.IntVar(&cfg.VNodeGoroutineLimit, "vnode-goroutine-limit", 32, "max concurrent goroutines per vnode")
	flag.DurationVar(&cfg.VNodeMaintenanceJitter, "vnode-maintenance-jitter", 5*time.Second, "vnode maintenance startup jitter range")
	flag.IntVar(&cfg.SiblingRouteMaxHops, "sibling-route-max-hops", 2, "max consecutive same-anchor vnode hops in routing")
	flag.Float64Var(&cfg.SuccessorListSiblingCap, "successor-list-sibling-cap", 0.5, "max fraction of successor list entries from same anchor")
	flag.BoolVar(&cfg.VNodeBootstrapPreferExt, "vnode-bootstrap-prefer-external", true, "prefer non-sibling bootstrap nodes when vnode joins")
	flag.IntVar(&cfg.SharedNodeInfoCacheSize, "shared-nodeinfo-cache-size", 2048, "L0 NodeInfo cache capacity")
	flag.DurationVar(&cfg.SharedNodeInfoCacheTTL, "shared-nodeinfo-cache-ttl", 30*time.Second, "L0 NodeInfo cache TTL")
	flag.DurationVar(&cfg.SharedRTTCacheTTL, "shared-rtt-cache-ttl", 60*time.Second, "L0 RTT cache TTL")
	flag.IntVar(&cfg.SharedRouteCacheSize, "shared-route-cache-size", 1024, "L0 routing LRU cache capacity")
	flag.DurationVar(&cfg.SharedRouteCacheTTL, "shared-route-cache-ttl", 10*time.Second, "L0 routing cache TTL")
	flag.IntVar(&cfg.SharedProofVerifyCacheSize, "shared-proof-verify-cache-size", 256, "L0 VNodeProof verify cache capacity")
	flag.DurationVar(&cfg.TransferTimeout, "transfer-timeout", 30*time.Second, "key transfer ACK timeout")

	// v3.0 flags
	flag.StringVar(&cfg.NodeRegion, "node-region", "", "node region label (auto-detected from tracker when empty)")
	flag.IntVar(&cfg.PredecessorListSize, "predecessor-list-size", chord.DefaultPredecessorListSize, "predecessor chain backup length")
	flag.IntVar(&cfg.FixFingersBatchSizeActive, "fix-fingers-batch-active", chord.DefaultFixFingersBatchSizeActive, "fingers repaired per cycle in active mode")
	flag.IntVar(&cfg.FixFingersBatchSizeQuiet, "fix-fingers-batch-quiet", chord.DefaultFixFingersBatchSizeQuiet, "fingers repaired per cycle in quiet mode")
	flag.BoolVar(&cfg.RoutingCacheEnabled, "routing-cache-enabled", true, "enable LRU routing result cache")
	flag.IntVar(&cfg.RoutingCacheSize, "routing-cache-size", chord.DefaultRoutingCacheSize, "routing cache max entries")
	flag.DurationVar(&cfg.RoutingCacheTTL, "routing-cache-ttl", chord.DefaultRoutingCacheTTL, "routing cache TTL")
	flag.Float64Var(&cfg.LatencyWeightID, "latency-weight-id", 0.6, "routing score weight for ID proximity")
	flag.Float64Var(&cfg.LatencyWeightRTT, "latency-weight-rtt", 0.3, "routing score weight for RTT")
	flag.Float64Var(&cfg.LatencyWeightRegion, "latency-weight-region", 0.1, "routing score weight for region affinity")
	flag.BoolVar(&cfg.ParallelLookupEnabled, "parallel-lookup-enabled", false, "enable parallel find_successor probing")
	flag.IntVar(&cfg.ParallelLookupCandidates, "parallel-lookup-candidates", 3, "parallel lookup candidate count")
	flag.DurationVar(&cfg.TimeoutPingSameRegion, "timeout-ping-same", chord.DefaultTimeoutPingSameRegion, "/ping timeout for same-region peers")
	flag.DurationVar(&cfg.TimeoutPingCrossRegion, "timeout-ping-cross", chord.DefaultTimeoutPingCrossRegion, "/ping timeout for cross-region peers")
	flag.DurationVar(&cfg.TimeoutFindSuccessorSame, "timeout-find-successor-same", chord.DefaultTimeoutFindSuccessorSame, "/find_successor timeout for same-region peers")
	flag.DurationVar(&cfg.TimeoutFindSuccessorCross, "timeout-find-successor-cross", chord.DefaultTimeoutFindSuccessorCross, "/find_successor timeout for cross-region peers")
	flag.DurationVar(&cfg.TimeoutFixFingersSame, "timeout-fix-fingers-same", chord.DefaultTimeoutFixFingersSame, "fix_fingers /find_successor timeout for same-region peers")
	flag.DurationVar(&cfg.TimeoutFixFingersCross, "timeout-fix-fingers-cross", chord.DefaultTimeoutFixFingersCross, "fix_fingers /find_successor timeout for cross-region peers")
	flag.DurationVar(&cfg.LatencyProbeIntervalActive, "latency-probe-interval-active", chord.DefaultLatencyProbeActiveInterval, "RTT probe interval in active mode")
	flag.DurationVar(&cfg.LatencyProbeIntervalQuiet, "latency-probe-interval-quiet", chord.DefaultLatencyProbeQuietInterval, "RTT probe interval in quiet mode")
	flag.Float64Var(&cfg.RTTEWMAAlpha, "rtt-ewma-alpha", chord.DefaultRTTEWMAAlpha, "EWMA smoothing factor for RTT samples")
	flag.DurationVar(&cfg.RTTSampleExpiry, "rtt-sample-expiry", chord.DefaultRTTSampleExpiry, "RTT sample expiry duration")
	flag.BoolVar(&cfg.PiggybackEnabled, "piggyback-enabled", true, "attach piggyback topology hints to responses")
	flag.IntVar(&cfg.StabilizeDebounceThreshold, "stabilize-debounce-threshold", chord.DefaultStabilizeDebounceThreshold, "consecutive stabilize changes before debounce")
	flag.DurationVar(&cfg.TopologyChangeWindow, "topology-change-window", chord.DefaultTopologyChangeWindow, "quiet period before switching to quiet maintenance mode")
	flag.DurationVar(&cfg.StabilizeActiveInterval, "stabilize-active-interval", chord.DefaultStabilizeActiveInterval, "stabilize interval in active mode")
	flag.DurationVar(&cfg.StabilizeQuietInterval, "stabilize-quiet-interval", chord.DefaultStabilizeQuietInterval, "stabilize interval in quiet mode")
	flag.DurationVar(&cfg.FixFingersActiveInterval, "fix-fingers-active-interval", chord.DefaultFixFingersActiveInterval, "fix_fingers interval in active mode")
	flag.DurationVar(&cfg.FixFingersQuietInterval, "fix-fingers-quiet-interval", chord.DefaultFixFingersQuietInterval, "fix_fingers interval in quiet mode")
	flag.DurationVar(&cfg.CheckPredecessorActiveInterval, "check-predecessor-active-interval", chord.DefaultCheckPredecessorActiveInterval, "check_predecessor interval in active mode")
	flag.DurationVar(&cfg.CheckPredecessorQuietInterval, "check-predecessor-quiet-interval", chord.DefaultCheckPredecessorQuietInterval, "check_predecessor interval in quiet mode")
	flag.Parse()

	// Env vars take priority over CLI flags.
	applyEnv("NODE_URI", &cfg.NodeURI)
	applyEnv("NODE_LISTEN", &cfg.ListenAddr)
	applyEnv("NODE_TLS_CERT_FILE", &cfg.TLSCertFile)
	applyEnv("NODE_TLS_KEY_FILE", &cfg.TLSKeyFile)
	applyEnvBool("CHORD_SKIP_TLS_VERIFY", &cfg.SkipTLSVerify)
	applyEnv("CHORD_LOG_LEVEL", &cfg.LogLevel)
	applyEnv("TRACKER_URL", &cfg.TrackerURL)
	applyEnv("NODE_MANUAL_SEEDS", &seeds)
	applyEnvDuration("CHORD_HTTP_TIMEOUT_SECONDS", &cfg.HTTPTimeout)
	applyEnvDuration("CHORD_MAINTENANCE_INTERVAL_SECONDS", &cfg.MaintenanceInterval)
	applyEnvInt("CHORD_SUCCESSOR_LIST_SIZE", &cfg.SuccessorListSize)
	applyEnvInt("CHORD_MAX_HOPS", &cfg.MaxHops)
	applyEnvInt("CHORD_SUSPICIOUS_THRESHOLD", &cfg.SuspiciousThreshold)
	applyEnvInt("CHORD_FAILED_THRESHOLD", &cfg.FailedThreshold)
	applyEnvInt("TRACKER_SEED_COUNT", &cfg.TrackerSeedCount)
	applyEnvDuration("CHORD_PING_LIVENESS_TIMEOUT_SECONDS", &cfg.PingLivenessTimeout)
	applyEnvBool("CHORD_STABILIZE_ATOMIC_STATE", &cfg.StabilizeAtomicState)
	applyEnvBool("CHORD_VALIDATE_AFTER_STABILIZE", &cfg.ValidateAfterStabilize)
	applyEnvBool("CHORD_RECTIFY_ENDPOINT_ALIAS", &cfg.RectifyEndpointAlias)
	applyEnvDuration("CHORD_INVARIANT_AUDIT_INTERVAL_SECONDS", &cfg.InvariantAuditInterval)
	applyEnvInt("CHORD_STABLE_BASE_MIN_SIZE", &cfg.StableBaseMinSize)
	applyEnv("CHORD_STABLE_BASE_MEMBERS", &stableBaseMembers)
	applyEnvBool("CHORD_AUTH_ENABLED", &cfg.Auth.Enabled)
	applyEnv("CHORD_AUTH_CA_PUBLIC_KEY_BASE64", &cfg.Auth.CAPublicKeyBase64)
	applyEnv("CHORD_AUTH_NODE_CERT_FILE", &cfg.Auth.NodeCertificateFile)
	applyEnv("CHORD_AUTH_NODE_PRIVATE_KEY_FILE", &cfg.Auth.NodePrivateKeyFile)
	applyEnvInt("CHORD_AUTH_TIMESTAMP_TOLERANCE", &cfg.Auth.TimestampToleranceSecs)
	applyEnvInt("CHORD_AUTH_NONCE_CACHE_TTL", &cfg.Auth.NonceCacheTTLSecs)
	applyEnvInt("CHORD_AUTH_NONCE_CACHE_MAX_SIZE", &cfg.Auth.NonceCacheMaxSize)
	applyEnvInt("CHORD_AUTH_CERT_CACHE_TTL", &cfg.Auth.CertCacheTTLSecs)
	applyEnv("CHORD_AUTH_CRL_FILE", &cfg.Auth.CRLFile)
	applyEnvBool("CHORD_AUTH_CRL_REFRESH", &cfg.Auth.CRLRefreshFromTracker)
	applyEnvInt("CHORD_AUTH_CERT_EXPIRY_WARN", &cfg.Auth.CertExpiryWarnDays)
	applyEnvInt("CHORD_AUTH_BOOT_GRACE", &cfg.Auth.BootGracePeriodSecs)
	applyEnvInt("CHORD_VNODE_COUNT", &cfg.VNodeCount)
	applyEnvInt("CHORD_MAX_VNODES", &cfg.MaxVNodes)
	applyEnvDuration("CHORD_VNODEPROOF_TTL", &cfg.VNodeProofTTL)
	applyEnvDuration("CHORD_VNODEPROOF_RENEW_BEFORE", &cfg.VNodeProofRenewBefore)
	applyEnvDuration("CHORD_CLOCK_SKEW_TOLERANCE", &cfg.ClockSkewTolerance)
	applyEnvInt("CHORD_VNODE_GOROUTINE_LIMIT", &cfg.VNodeGoroutineLimit)
	applyEnvDuration("CHORD_VNODE_MAINTENANCE_JITTER", &cfg.VNodeMaintenanceJitter)
	applyEnvInt("CHORD_SIBLING_ROUTE_MAX_HOPS", &cfg.SiblingRouteMaxHops)
	applyEnvFloat("CHORD_SUCCESSOR_LIST_SIBLING_CAP", &cfg.SuccessorListSiblingCap)
	applyEnvBool("CHORD_VNODE_BOOTSTRAP_PREFER_EXT", &cfg.VNodeBootstrapPreferExt)
	applyEnvInt("CHORD_SHARED_NODEINFO_CACHE_SIZE", &cfg.SharedNodeInfoCacheSize)
	applyEnvDuration("CHORD_SHARED_NODEINFO_CACHE_TTL", &cfg.SharedNodeInfoCacheTTL)
	applyEnvDuration("CHORD_SHARED_RTT_CACHE_TTL", &cfg.SharedRTTCacheTTL)
	applyEnvInt("CHORD_SHARED_ROUTE_CACHE_SIZE", &cfg.SharedRouteCacheSize)
	applyEnvDuration("CHORD_SHARED_ROUTE_CACHE_TTL", &cfg.SharedRouteCacheTTL)
	applyEnvInt("CHORD_SHARED_PROOF_VERIFY_CACHE_SIZE", &cfg.SharedProofVerifyCacheSize)
	applyEnvDuration("CHORD_TRANSFER_TIMEOUT", &cfg.TransferTimeout)
	applyEnv("CHORD_NODE_REGION", &cfg.NodeRegion)
	applyEnvInt("CHORD_PREDECESSOR_LIST_SIZE", &cfg.PredecessorListSize)
	applyEnvInt("CHORD_FIX_FINGERS_BATCH_ACTIVE", &cfg.FixFingersBatchSizeActive)
	applyEnvInt("CHORD_FIX_FINGERS_BATCH_QUIET", &cfg.FixFingersBatchSizeQuiet)
	applyEnvBool("CHORD_ROUTING_CACHE_ENABLED", &cfg.RoutingCacheEnabled)
	applyEnvInt("CHORD_ROUTING_CACHE_SIZE", &cfg.RoutingCacheSize)
	applyEnvDuration("CHORD_ROUTING_CACHE_TTL_SECONDS", &cfg.RoutingCacheTTL)
	applyEnvFloat("CHORD_LATENCY_WEIGHT_ID", &cfg.LatencyWeightID)
	applyEnvFloat("CHORD_LATENCY_WEIGHT_RTT", &cfg.LatencyWeightRTT)
	applyEnvFloat("CHORD_LATENCY_WEIGHT_REGION", &cfg.LatencyWeightRegion)
	applyEnvBool("CHORD_PARALLEL_LOOKUP_ENABLED", &cfg.ParallelLookupEnabled)
	applyEnvInt("CHORD_PARALLEL_LOOKUP_CANDIDATES", &cfg.ParallelLookupCandidates)
	applyEnvDuration("CHORD_TIMEOUT_PING_SAME", &cfg.TimeoutPingSameRegion)
	applyEnvDuration("CHORD_TIMEOUT_PING_CROSS", &cfg.TimeoutPingCrossRegion)
	applyEnvDuration("CHORD_TIMEOUT_FIND_SUCCESSOR_SAME", &cfg.TimeoutFindSuccessorSame)
	applyEnvDuration("CHORD_TIMEOUT_FIND_SUCCESSOR_CROSS", &cfg.TimeoutFindSuccessorCross)
	applyEnvDuration("CHORD_TIMEOUT_FIX_FINGERS_SAME", &cfg.TimeoutFixFingersSame)
	applyEnvDuration("CHORD_TIMEOUT_FIX_FINGERS_CROSS", &cfg.TimeoutFixFingersCross)
	applyEnvDuration("CHORD_LATENCY_PROBE_ACTIVE", &cfg.LatencyProbeIntervalActive)
	applyEnvDuration("CHORD_LATENCY_PROBE_QUIET", &cfg.LatencyProbeIntervalQuiet)
	applyEnvFloat("CHORD_RTT_EWMA_ALPHA", &cfg.RTTEWMAAlpha)
	applyEnvDuration("CHORD_RTT_SAMPLE_EXPIRY", &cfg.RTTSampleExpiry)
	applyEnvBool("CHORD_PIGGYBACK_ENABLED", &cfg.PiggybackEnabled)
	applyEnvInt("CHORD_STABILIZE_DEBOUNCE", &cfg.StabilizeDebounceThreshold)
	applyEnvDuration("CHORD_TOPOLOGY_CHANGE_WINDOW", &cfg.TopologyChangeWindow)
	applyEnvDuration("CHORD_STABILIZE_ACTIVE_INTERVAL", &cfg.StabilizeActiveInterval)
	applyEnvDuration("CHORD_STABILIZE_QUIET_INTERVAL", &cfg.StabilizeQuietInterval)
	applyEnvDuration("CHORD_FIX_FINGERS_ACTIVE_INTERVAL", &cfg.FixFingersActiveInterval)
	applyEnvDuration("CHORD_FIX_FINGERS_QUIET_INTERVAL", &cfg.FixFingersQuietInterval)
	applyEnvDuration("CHORD_CHECK_PREDECESSOR_ACTIVE_INTERVAL", &cfg.CheckPredecessorActiveInterval)
	applyEnvDuration("CHORD_CHECK_PREDECESSOR_QUIET_INTERVAL", &cfg.CheckPredecessorQuietInterval)

	normalized, err := chord.NormalizeURI(cfg.NodeURI)
	if err != nil {
		return Config{}, err
	}
	cfg.NodeURI = normalized
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = listenFromURI(cfg.NodeURI)
	}
	if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
		return Config{}, errors.New("strict https mode requires -tls-cert and -tls-key")
	}
	cfg.LogLevel, err = logging.NormalizeLevel(cfg.LogLevel)
	if err != nil {
		return Config{}, err
	}
	if cfg.TrackerURL != "" {
		tracker, err := chord.NormalizeURI(cfg.TrackerURL)
		if err != nil {
			return Config{}, err
		}
		cfg.TrackerURL = tracker
	}
	for _, seed := range strings.Split(seeds, ",") {
		seed = strings.TrimSpace(seed)
		if seed == "" {
			continue
		}
		normalizedSeed, err := chord.NormalizeURI(seed)
		if err != nil {
			return Config{}, err
		}
		cfg.ManualSeeds = append(cfg.ManualSeeds, normalizedSeed)
	}
	for _, member := range strings.Split(stableBaseMembers, ",") {
		member = strings.TrimSpace(member)
		if member == "" {
			continue
		}
		normalizedMember, err := chord.NormalizeURI(member)
		if err != nil {
			return Config{}, err
		}
		cfg.StableBaseMembers = append(cfg.StableBaseMembers, normalizedMember)
	}
	if cfg.StableBaseMinSize <= 0 {
		cfg.StableBaseMinSize = cfg.SuccessorListSize + 1
	}
	if cfg.Auth.Enabled {
		if cfg.Auth.CAPublicKeyBase64 == "" {
			return Config{}, errors.New("auth.ca-public-key-base64 is required when auth is enabled")
		}
		if cfg.Auth.NodeCertificateFile == "" {
			return Config{}, errors.New("auth.node-certificate-file is required when auth is enabled")
		}
		if cfg.Auth.NodePrivateKeyFile == "" {
			return Config{}, errors.New("auth.node-private-key-file is required when auth is enabled")
		}
	}
	return cfg, nil
}

func (c Config) ChordOptions() chord.Options {
	return chord.Options{
		// v4.0 vnode options
		MaxVNodes:                  c.MaxVNodes,
		VNodeProofTTL:              c.VNodeProofTTL,
		VNodeProofRenewBefore:      c.VNodeProofRenewBefore,
		ClockSkewTolerance:         c.ClockSkewTolerance,
		VNodeGoroutineLimit:        c.VNodeGoroutineLimit,
		VNodeMaintenanceJitter:     c.VNodeMaintenanceJitter,
		SiblingRouteMaxHops:        c.SiblingRouteMaxHops,
		SuccessorListSiblingCap:    c.SuccessorListSiblingCap,
		VNodeBootstrapPreferExt:    c.VNodeBootstrapPreferExt,
		SharedNodeInfoCacheSize:    c.SharedNodeInfoCacheSize,
		SharedNodeInfoCacheTTL:     c.SharedNodeInfoCacheTTL,
		SharedRTTCacheTTL:          c.SharedRTTCacheTTL,
		SharedRouteCacheSize:       c.SharedRouteCacheSize,
		SharedRouteCacheTTL:        c.SharedRouteCacheTTL,
		SharedProofVerifyCacheSize: c.SharedProofVerifyCacheSize,
		TransferTimeout:            c.TransferTimeout,

		SuccessorListSize:      c.SuccessorListSize,
		MaintenanceInterval:    c.MaintenanceInterval,
		MaxHops:                c.MaxHops,
		SuspiciousThreshold:    c.SuspiciousThreshold,
		FailedThreshold:        c.FailedThreshold,
		TrackerSeedCount:       c.TrackerSeedCount,
		PingLivenessTimeout:    c.PingLivenessTimeout,
		StabilizeAtomicState:   c.StabilizeAtomicState,
		ValidateAfterStabilize: c.ValidateAfterStabilize,
		RectifyEndpointAlias:   c.RectifyEndpointAlias,
		InvariantAuditInterval: c.InvariantAuditInterval,
		StableBaseMinSize:      c.StableBaseMinSize,
		StableBaseMembers:      c.StableBaseMembers,

		Region:                         c.NodeRegion,
		PredecessorListSize:            c.PredecessorListSize,
		FixFingersBatchSizeActive:      c.FixFingersBatchSizeActive,
		FixFingersBatchSizeQuiet:       c.FixFingersBatchSizeQuiet,
		RoutingCacheEnabled:            c.RoutingCacheEnabled,
		RoutingCacheSize:               c.RoutingCacheSize,
		RoutingCacheTTL:                c.RoutingCacheTTL,
		LatencyWeightID:                c.LatencyWeightID,
		LatencyWeightRTT:               c.LatencyWeightRTT,
		LatencyWeightRegion:            c.LatencyWeightRegion,
		ParallelLookupEnabled:          c.ParallelLookupEnabled,
		ParallelLookupCandidates:       c.ParallelLookupCandidates,
		TimeoutPingSameRegion:          c.TimeoutPingSameRegion,
		TimeoutPingCrossRegion:         c.TimeoutPingCrossRegion,
		TimeoutFindSuccessorSame:       c.TimeoutFindSuccessorSame,
		TimeoutFindSuccessorCross:      c.TimeoutFindSuccessorCross,
		TimeoutFixFingersSame:          c.TimeoutFixFingersSame,
		TimeoutFixFingersCross:         c.TimeoutFixFingersCross,
		LatencyProbeIntervalActive:     c.LatencyProbeIntervalActive,
		LatencyProbeIntervalQuiet:      c.LatencyProbeIntervalQuiet,
		RTTEWMAAlpha:                   c.RTTEWMAAlpha,
		RTTSampleExpiry:                c.RTTSampleExpiry,
		PiggybackEnabled:               c.PiggybackEnabled,
		StabilizeDebounceThreshold:     c.StabilizeDebounceThreshold,
		TopologyChangeWindow:           c.TopologyChangeWindow,
		StabilizeActiveInterval:        c.StabilizeActiveInterval,
		StabilizeQuietInterval:         c.StabilizeQuietInterval,
		FixFingersActiveInterval:       c.FixFingersActiveInterval,
		FixFingersQuietInterval:        c.FixFingersQuietInterval,
		CheckPredecessorActiveInterval: c.CheckPredecessorActiveInterval,
		CheckPredecessorQuietInterval:  c.CheckPredecessorQuietInterval,
	}
}

func applyEnv(key string, target *string) {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		*target = v
	}
}

func applyEnvInt(key string, target *int) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return
	}
	if parsed, err := strconv.Atoi(value); err == nil {
		*target = parsed
	}
}

func applyEnvBool(key string, target *bool) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return
	}
	if parsed, err := strconv.ParseBool(value); err == nil {
		*target = parsed
	}
}

func applyEnvFloat(key string, target *float64) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return
	}
	if parsed, err := strconv.ParseFloat(value, 64); err == nil {
		*target = parsed
	}
}

func applyEnvDuration(key string, target *time.Duration) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		*target = time.Duration(seconds) * time.Second
	}
}

func listenFromURI(uri string) string {
	host, port, err := net.SplitHostPort(strings.TrimPrefix(uri, "https://"))
	if err != nil {
		return ":443"
	}
	if host == "" {
		return ":" + port
	}
	return ":" + port
}
