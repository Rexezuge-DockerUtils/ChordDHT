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
