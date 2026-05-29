package client

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"chorddht/internal/auth"
	"chorddht/internal/chord"
	"chorddht/internal/logging"
)

type jsonClient struct {
	baseURL string
	http    *http.Client
	signer  *auth.RequestSigner // nil when auth is disabled
}

func newJSONClient(baseURL string, timeout time.Duration, skipTLSVerify bool, signer *auth.RequestSigner) (jsonClient, error) {
	normalized, err := chord.NormalizeURI(baseURL)
	if err != nil {
		return jsonClient{}, err
	}
	if timeout <= 0 {
		timeout = chord.DefaultHTTPTimeout
	}
	httpClient := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if skipTLSVerify {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		httpClient.Transport = transport
	}
	return jsonClient{
		baseURL: normalized,
		http:    httpClient,
		signer:  signer,
	}, nil
}

func (c jsonClient) do(method, path string, in any, out any) error {
	return c.doSigned(method, path, in, out, false)
}

// doSigned performs an HTTP request. If the client has a signer, auth headers are added.
// When includeCert is true, X-Chord-Certificate is also added.
func (c jsonClient) doSigned(method, path string, in any, out any, includeCert bool) error {
	var bodyBytes []byte
	if in != nil {
		var err error
		bodyBytes, err = json.Marshal(in)
		if err != nil {
			return err
		}
		// json.Marshal omits trailing newline; add one to match json.Encoder behaviour.
		bodyBytes = append(bodyBytes, '\n')
	}

	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if bodyBytes != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.signer != nil {
		if err := c.signer.Sign(req, bodyBytes); err != nil {
			return fmt.Errorf("sign request: %w", err)
		}
		if includeCert {
			if err := c.signer.AddCertHeader(req); err != nil {
				return fmt.Errorf("add cert header: %w", err)
			}
		}
	}

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		logging.Warnf("outbound request failed method=%s url=%s error=%v duration=%s", method, req.URL.String(), err, time.Since(start))
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		logging.Warnf("outbound request returned error method=%s url=%s status=%d duration=%s", method, req.URL.String(), resp.StatusCode, time.Since(start))
		return decodeAPIError(resp)
	}
	if out == nil {
		logging.Debugf("outbound request completed method=%s url=%s status=%d duration=%s", method, req.URL.String(), resp.StatusCode, time.Since(start))
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		logging.Warnf("outbound response decode failed method=%s url=%s status=%d error=%v duration=%s", method, req.URL.String(), resp.StatusCode, err, time.Since(start))
		return err
	}
	logging.Debugf("outbound request completed method=%s url=%s status=%d duration=%s", method, req.URL.String(), resp.StatusCode, time.Since(start))
	return nil
}

func decodeAPIError(resp *http.Response) error {
	var payload struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Detail  map[string]any `json:"detail"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil || payload.Error.Code == "" {
		return chord.NewAPIError(resp.StatusCode, chord.ErrUpstream, fmt.Sprintf("upstream returned HTTP %d", resp.StatusCode))
	}
	return &chord.APIError{StatusCode: resp.StatusCode, Code: payload.Error.Code, Message: payload.Error.Message, Detail: payload.Error.Detail}
}

// isCertRequired reports whether err is a CERTIFICATE_REQUIRED API error.
func isCertRequired(err error) bool {
	var apiErr *chord.APIError
	return errors.As(err, &apiErr) && apiErr.Code == chord.ErrCertificateRequired
}

func appendQuery(path string, values url.Values) string {
	query := values.Encode()
	if query == "" {
		return path
	}
	return path + "?" + query
}

// timeNow is a package-level alias so tests can stub it if needed.
var timeNow = time.Now

func requireHTTPSURI(uri string) error {
	normalized, err := chord.NormalizeURI(uri)
	if err != nil {
		return err
	}
	if normalized != uri {
		return errors.New("uri must be normalized https URI")
	}
	if !strings.HasPrefix(uri, "https://") {
		return errors.New("uri must use https scheme")
	}
	return nil
}
