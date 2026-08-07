package registry

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultHTTPTimeout = 30 * time.Second

type (
	// Option configures a Client.
	Option func(*clientOptions) error

	clientOptions struct {
		baseURL    *url.URL
		httpClient *http.Client
	}
)

// WithBaseURL sets the root URL of the static registry distribution.
func WithBaseURL(value string) Option {
	return func(options *clientOptions) error {
		parsed, err := parseBaseURL(value)
		if err != nil {
			return err
		}

		options.baseURL = parsed

		return nil
	}
}

// WithHTTPClient sets the HTTP client used for all registry requests.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) error {
		if client == nil {
			return fmt.Errorf("registry HTTP client is nil")
		}

		options.httpClient = client

		return nil
	}
}

func defaultClientOptions() (clientOptions, error) {
	baseURL, err := parseBaseURL(DefaultBaseURL)
	if err != nil {
		return clientOptions{}, err
	}

	return clientOptions{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
	}, nil
}

func parseBaseURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("parse registry base URL: %w", err)
	}

	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("registry base URL must be an absolute HTTP or HTTPS URL")
	}

	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("registry base URL must not contain credentials, a query, or a fragment")
	}

	if !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
	}

	return parsed, nil
}
