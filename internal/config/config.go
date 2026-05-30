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

func envFloat(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

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
	NodeURI             string
	ListenAddr          string
	TLSCertFile         string
	TLSKeyFile          string
	SkipTLSVerify       bool
	LogLevel            string
	TrackerURL          string
	ManualSeeds         []string
	HTTPTimeout         time.Duration
	MaintenanceInterval time.Duration
	SuccessorListSize   int
	MaxHops             int
	SuspiciousThreshold int
	FailedThreshold     int
	TrackerSeedCount    int
	Auth                AuthConfig

	// v3.0 fields
	NodeRegion                         string
	PredecessorListSize                int
	FixFingersBatchSizeActive          int
	FixFingersBatchSizeQuiet           int
	RoutingCacheEnabled                bool
	RoutingCacheSize                   int
	RoutingCacheTTL                    time.Duration
	LatencyWeightID                    float64
	LatencyWeightRTT                   float64
	LatencyWeightRegion                float64
	ParallelLookupEnabled              bool
	ParallelLookupCandidates           int
	TimeoutPingSameRegion              time.Duration
	TimeoutPingCrossRegion             time.Duration
	TimeoutFindSuccessorSame           time.Duration
	TimeoutFindSuccessorCross          time.Duration
	TimeoutFixFingersSame              time.Duration
	TimeoutFixFingersCross             time.Duration
	LatencyProbeIntervalActive         time.Duration
	LatencyProbeIntervalQuiet          time.Duration
	RTTEWMAAlpha                       float64
	RTTSampleExpiry                    time.Duration
	PiggybackEnabled                   bool
	StabilizeDebounceThreshold         int
	TopologyChangeWindow               time.Duration
	StabilizeActiveInterval            time.Duration
	StabilizeQuietInterval             time.Duration
	FixFingersActiveInterval           time.Duration
	FixFingersQuietInterval            time.Duration
	CheckPredecessorActiveInterval     time.Duration
	CheckPredecessorQuietInterval      time.Duration
}

func Load() (Config, error) {
	var seeds string
	cfg := Config{}
	flag.StringVar(&cfg.NodeURI, "uri", env("NODE_URI", ""), "canonical node https:// URI")
	flag.StringVar(&cfg.ListenAddr, "listen", env("NODE_LISTEN", ""), "TLS listen address, defaults to URI port")
	flag.StringVar(&cfg.TLSCertFile, "tls-cert", env("NODE_TLS_CERT_FILE", ""), "TLS certificate file")
	flag.StringVar(&cfg.TLSKeyFile, "tls-key", env("NODE_TLS_KEY_FILE", ""), "TLS private key file")
	flag.BoolVar(&cfg.SkipTLSVerify, "skip-tls-verify", envBool("CHORD_SKIP_TLS_VERIFY", false), "skip outbound TLS certificate verification")
	flag.StringVar(&cfg.LogLevel, "log-level", env("CHORD_LOG_LEVEL", "info"), "log level: debug, info, warn, error")
	flag.StringVar(&cfg.TrackerURL, "tracker-url", env("TRACKER_URL", ""), "optional tracker https:// URI")
	flag.StringVar(&seeds, "seeds", env("NODE_MANUAL_SEEDS", ""), "comma-separated manual seed https:// URIs")
	flag.DurationVar(&cfg.HTTPTimeout, "http-timeout", envDuration("CHORD_HTTP_TIMEOUT_SECONDS", chord.DefaultHTTPTimeout), "HTTP request timeout")
	flag.DurationVar(&cfg.MaintenanceInterval, "maintenance-interval", envDuration("CHORD_MAINTENANCE_INTERVAL_SECONDS", chord.DefaultMaintenanceInterval), "maintenance interval")
	flag.IntVar(&cfg.SuccessorListSize, "successor-list-size", envInt("CHORD_SUCCESSOR_LIST_SIZE", chord.DefaultSuccessorListSize), "successor list size")
	flag.IntVar(&cfg.MaxHops, "max-hops", envInt("CHORD_MAX_HOPS", chord.DefaultMaxHops), "maximum find_successor hops")
	flag.IntVar(&cfg.SuspiciousThreshold, "suspicious-threshold", envInt("CHORD_SUSPICIOUS_THRESHOLD", chord.DefaultSuspiciousThreshold), "suspicious threshold")
	flag.IntVar(&cfg.FailedThreshold, "failed-threshold", envInt("CHORD_FAILED_THRESHOLD", chord.DefaultFailedThreshold), "failed threshold")
	flag.IntVar(&cfg.TrackerSeedCount, "tracker-seed-count", envInt("TRACKER_SEED_COUNT", chord.DefaultTrackerSeedCount), "tracker seed count")
	flag.BoolVar(&cfg.Auth.Enabled, "auth.enabled", envBool("CHORD_AUTH_ENABLED", false), "enable node identity authentication")
	flag.StringVar(&cfg.Auth.CAPublicKeyBase64, "auth.ca-public-key-base64", env("CHORD_AUTH_CA_PUBLIC_KEY_BASE64", ""), "CA Ed25519 public key (base64url)")
	flag.StringVar(&cfg.Auth.NodeCertificateFile, "auth.node-certificate-file", env("CHORD_AUTH_NODE_CERT_FILE", ""), "node certificate JSON file")
	flag.StringVar(&cfg.Auth.NodePrivateKeyFile, "auth.node-private-key-file", env("CHORD_AUTH_NODE_PRIVATE_KEY_FILE", ""), "node Ed25519 private key file (base64url)")
	flag.IntVar(&cfg.Auth.TimestampToleranceSecs, "auth.timestamp-tolerance-secs", envInt("CHORD_AUTH_TIMESTAMP_TOLERANCE", 300), "request timestamp tolerance (seconds)")
	flag.IntVar(&cfg.Auth.NonceCacheTTLSecs, "auth.nonce-cache-ttl-secs", envInt("CHORD_AUTH_NONCE_CACHE_TTL", 600), "nonce cache TTL (seconds)")
	flag.IntVar(&cfg.Auth.NonceCacheMaxSize, "auth.nonce-cache-max-size", envInt("CHORD_AUTH_NONCE_CACHE_MAX_SIZE", 10000), "nonce cache max entries")
	flag.IntVar(&cfg.Auth.CertCacheTTLSecs, "auth.cert-cache-ttl-secs", envInt("CHORD_AUTH_CERT_CACHE_TTL", 3600), "cert cache TTL (seconds)")
	flag.StringVar(&cfg.Auth.CRLFile, "auth.crl-file", env("CHORD_AUTH_CRL_FILE", ""), "CRL JSON file path")
	flag.BoolVar(&cfg.Auth.CRLRefreshFromTracker, "auth.crl-refresh-from-tracker", envBool("CHORD_AUTH_CRL_REFRESH", true), "auto-refresh CRL from tracker")
	flag.IntVar(&cfg.Auth.CertExpiryWarnDays, "auth.cert-expiry-warn-days", envInt("CHORD_AUTH_CERT_EXPIRY_WARN", 30), "days before cert expiry to warn")
	flag.IntVar(&cfg.Auth.BootGracePeriodSecs, "auth.boot-grace-period-secs", envInt("CHORD_AUTH_BOOT_GRACE", 0), "seconds after startup before auth is enforced")

	// v3.0 flags
	flag.StringVar(&cfg.NodeRegion, "node-region", env("CHORD_NODE_REGION", ""), "node region label (auto-detected from tracker when empty)")
	flag.IntVar(&cfg.PredecessorListSize, "predecessor-list-size", envInt("CHORD_PREDECESSOR_LIST_SIZE", chord.DefaultPredecessorListSize), "predecessor chain backup length")
	flag.IntVar(&cfg.FixFingersBatchSizeActive, "fix-fingers-batch-active", envInt("CHORD_FIX_FINGERS_BATCH_ACTIVE", chord.DefaultFixFingersBatchSizeActive), "fingers repaired per cycle in active mode")
	flag.IntVar(&cfg.FixFingersBatchSizeQuiet, "fix-fingers-batch-quiet", envInt("CHORD_FIX_FINGERS_BATCH_QUIET", chord.DefaultFixFingersBatchSizeQuiet), "fingers repaired per cycle in quiet mode")
	flag.BoolVar(&cfg.RoutingCacheEnabled, "routing-cache-enabled", envBool("CHORD_ROUTING_CACHE_ENABLED", true), "enable LRU routing result cache")
	flag.IntVar(&cfg.RoutingCacheSize, "routing-cache-size", envInt("CHORD_ROUTING_CACHE_SIZE", chord.DefaultRoutingCacheSize), "routing cache max entries")
	flag.DurationVar(&cfg.RoutingCacheTTL, "routing-cache-ttl", envDuration("CHORD_ROUTING_CACHE_TTL_SECONDS", chord.DefaultRoutingCacheTTL), "routing cache TTL")
	flag.Float64Var(&cfg.LatencyWeightID, "latency-weight-id", envFloat("CHORD_LATENCY_WEIGHT_ID", 0.6), "routing score weight for ID proximity")
	flag.Float64Var(&cfg.LatencyWeightRTT, "latency-weight-rtt", envFloat("CHORD_LATENCY_WEIGHT_RTT", 0.3), "routing score weight for RTT")
	flag.Float64Var(&cfg.LatencyWeightRegion, "latency-weight-region", envFloat("CHORD_LATENCY_WEIGHT_REGION", 0.1), "routing score weight for region affinity")
	flag.BoolVar(&cfg.ParallelLookupEnabled, "parallel-lookup-enabled", envBool("CHORD_PARALLEL_LOOKUP_ENABLED", false), "enable parallel find_successor probing")
	flag.IntVar(&cfg.ParallelLookupCandidates, "parallel-lookup-candidates", envInt("CHORD_PARALLEL_LOOKUP_CANDIDATES", 3), "parallel lookup candidate count")
	flag.DurationVar(&cfg.TimeoutPingSameRegion, "timeout-ping-same", envDuration("CHORD_TIMEOUT_PING_SAME", chord.DefaultTimeoutPingSameRegion), "/ping timeout for same-region peers")
	flag.DurationVar(&cfg.TimeoutPingCrossRegion, "timeout-ping-cross", envDuration("CHORD_TIMEOUT_PING_CROSS", chord.DefaultTimeoutPingCrossRegion), "/ping timeout for cross-region peers")
	flag.DurationVar(&cfg.TimeoutFindSuccessorSame, "timeout-find-successor-same", envDuration("CHORD_TIMEOUT_FIND_SUCCESSOR_SAME", chord.DefaultTimeoutFindSuccessorSame), "/find_successor timeout for same-region peers")
	flag.DurationVar(&cfg.TimeoutFindSuccessorCross, "timeout-find-successor-cross", envDuration("CHORD_TIMEOUT_FIND_SUCCESSOR_CROSS", chord.DefaultTimeoutFindSuccessorCross), "/find_successor timeout for cross-region peers")
	flag.DurationVar(&cfg.TimeoutFixFingersSame, "timeout-fix-fingers-same", envDuration("CHORD_TIMEOUT_FIX_FINGERS_SAME", chord.DefaultTimeoutFixFingersSame), "fix_fingers /find_successor timeout for same-region peers")
	flag.DurationVar(&cfg.TimeoutFixFingersCross, "timeout-fix-fingers-cross", envDuration("CHORD_TIMEOUT_FIX_FINGERS_CROSS", chord.DefaultTimeoutFixFingersCross), "fix_fingers /find_successor timeout for cross-region peers")
	flag.DurationVar(&cfg.LatencyProbeIntervalActive, "latency-probe-interval-active", envDuration("CHORD_LATENCY_PROBE_ACTIVE", chord.DefaultLatencyProbeActiveInterval), "RTT probe interval in active mode")
	flag.DurationVar(&cfg.LatencyProbeIntervalQuiet, "latency-probe-interval-quiet", envDuration("CHORD_LATENCY_PROBE_QUIET", chord.DefaultLatencyProbeQuietInterval), "RTT probe interval in quiet mode")
	flag.Float64Var(&cfg.RTTEWMAAlpha, "rtt-ewma-alpha", envFloat("CHORD_RTT_EWMA_ALPHA", chord.DefaultRTTEWMAAlpha), "EWMA smoothing factor for RTT samples")
	flag.DurationVar(&cfg.RTTSampleExpiry, "rtt-sample-expiry", envDuration("CHORD_RTT_SAMPLE_EXPIRY", chord.DefaultRTTSampleExpiry), "RTT sample expiry duration")
	flag.BoolVar(&cfg.PiggybackEnabled, "piggyback-enabled", envBool("CHORD_PIGGYBACK_ENABLED", true), "attach piggyback topology hints to responses")
	flag.IntVar(&cfg.StabilizeDebounceThreshold, "stabilize-debounce-threshold", envInt("CHORD_STABILIZE_DEBOUNCE", chord.DefaultStabilizeDebounceThreshold), "consecutive stabilize changes before debounce")
	flag.DurationVar(&cfg.TopologyChangeWindow, "topology-change-window", envDuration("CHORD_TOPOLOGY_CHANGE_WINDOW", chord.DefaultTopologyChangeWindow), "quiet period before switching to quiet maintenance mode")
	flag.DurationVar(&cfg.StabilizeActiveInterval, "stabilize-active-interval", envDuration("CHORD_STABILIZE_ACTIVE_INTERVAL", chord.DefaultStabilizeActiveInterval), "stabilize interval in active mode")
	flag.DurationVar(&cfg.StabilizeQuietInterval, "stabilize-quiet-interval", envDuration("CHORD_STABILIZE_QUIET_INTERVAL", chord.DefaultStabilizeQuietInterval), "stabilize interval in quiet mode")
	flag.DurationVar(&cfg.FixFingersActiveInterval, "fix-fingers-active-interval", envDuration("CHORD_FIX_FINGERS_ACTIVE_INTERVAL", chord.DefaultFixFingersActiveInterval), "fix_fingers interval in active mode")
	flag.DurationVar(&cfg.FixFingersQuietInterval, "fix-fingers-quiet-interval", envDuration("CHORD_FIX_FINGERS_QUIET_INTERVAL", chord.DefaultFixFingersQuietInterval), "fix_fingers interval in quiet mode")
	flag.DurationVar(&cfg.CheckPredecessorActiveInterval, "check-predecessor-active-interval", envDuration("CHORD_CHECK_PREDECESSOR_ACTIVE_INTERVAL", chord.DefaultCheckPredecessorActiveInterval), "check_predecessor interval in active mode")
	flag.DurationVar(&cfg.CheckPredecessorQuietInterval, "check-predecessor-quiet-interval", envDuration("CHORD_CHECK_PREDECESSOR_QUIET_INTERVAL", chord.DefaultCheckPredecessorQuietInterval), "check_predecessor interval in quiet mode")
	flag.Parse()

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
		SuccessorListSize:   c.SuccessorListSize,
		MaintenanceInterval: c.MaintenanceInterval,
		MaxHops:             c.MaxHops,
		SuspiciousThreshold: c.SuspiciousThreshold,
		FailedThreshold:     c.FailedThreshold,
		TrackerSeedCount:    c.TrackerSeedCount,

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

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return time.Duration(seconds) * time.Second
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
