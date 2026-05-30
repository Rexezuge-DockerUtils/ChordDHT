package regiondetect

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

var metadataClient = &http.Client{
	Timeout: 2 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// Detect probes Azure, AWS, and GCP instance metadata endpoints in parallel
// and returns the first region string that is successfully read. Returns ""
// if no provider responds within the given timeout.
func Detect(timeout time.Duration) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ch := make(chan string, 3)
	go func() { ch <- azure(ctx) }()
	go func() { ch <- aws(ctx) }()
	go func() { ch <- gcp(ctx) }()

	for i := 0; i < 3; i++ {
		select {
		case r := <-ch:
			if r != "" {
				return r
			}
		case <-ctx.Done():
			return ""
		}
	}
	return ""
}

func fetch(ctx context.Context, url string, headers map[string]string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := metadataClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// azure reads the location from Azure Instance Metadata Service.
// Returns e.g. "eastus", "westeurope".
func azure(ctx context.Context) string {
	return fetch(ctx,
		"http://169.254.169.254/metadata/instance/compute/location?api-version=2021-01-01&format=text",
		map[string]string{"Metadata": "true"})
}

// aws reads the region from AWS EC2 Instance Metadata Service.
// Returns e.g. "us-east-1", "eu-west-1".
func aws(ctx context.Context) string {
	return fetch(ctx, "http://169.254.169.254/latest/meta-data/placement/region", nil)
}

// gcp reads the zone from GCP Compute Engine metadata and strips the zone
// suffix to return the region. Returns e.g. "us-central1", "europe-west1".
func gcp(ctx context.Context) string {
	zone := fetch(ctx,
		"http://metadata.google.internal/computeMetadata/v1/instance/zone",
		map[string]string{"Metadata-Flavor": "Google"})
	if zone == "" {
		return ""
	}
	// Full form: "projects/PROJECT_ID/zones/ZONE" — take the last segment.
	parts := strings.Split(zone, "/")
	z := parts[len(parts)-1] // e.g. "us-central1-a"
	// Strip the trailing "-<letter>" zone suffix to get the region.
	if i := strings.LastIndex(z, "-"); i > 0 {
		return z[:i] // e.g. "us-central1"
	}
	return z
}
