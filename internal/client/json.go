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

	"chorddht/internal/chord"
	"chorddht/internal/logging"
)

type jsonClient struct {
	baseURL string
	http    *http.Client
}

func newJSONClient(baseURL string, timeout time.Duration, skipTLSVerify bool) (jsonClient, error) {
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
	}, nil
}

func (c jsonClient) do(method, path string, in any, out any) error {
	var body io.Reader
	if in != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(in); err != nil {
			return err
		}
		body = buf
	}
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
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

func appendQuery(path string, values url.Values) string {
	query := values.Encode()
	if query == "" {
		return path
	}
	return path + "?" + query
}

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
