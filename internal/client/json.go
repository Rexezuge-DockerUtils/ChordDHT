package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"chorddht/internal/chord"
)

type jsonClient struct {
	baseURL string
	http    *http.Client
}

func newJSONClient(baseURL string, timeout time.Duration) (jsonClient, error) {
	normalized, err := chord.NormalizeURI(baseURL)
	if err != nil {
		return jsonClient{}, err
	}
	if timeout <= 0 {
		timeout = chord.DefaultHTTPTimeout
	}
	return jsonClient{
		baseURL: normalized,
		http: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
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
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return decodeAPIError(resp)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
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
