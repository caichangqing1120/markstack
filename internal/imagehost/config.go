package imagehost

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Mode string

const (
	ModeLocal Mode = "local"
	ModeHTTP  Mode = "http"
	ModeS3    Mode = "s3"
)

type Config struct {
	Mode Mode       `json:"mode"`
	HTTP HTTPConfig `json:"http"`
	S3   S3Config   `json:"s3"`
}

type HTTPConfig struct {
	Endpoint  string            `json:"endpoint"`
	Method    string            `json:"method"`
	FileField string            `json:"fileField"`
	Headers   map[string]string `json:"headers,omitempty"`
	JSONPath  string            `json:"jsonPath"`
}

type S3Config struct {
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"accessKeyID"`
	SecretAccessKey string `json:"secretAccessKey"`
	PathPrefix      string `json:"pathPrefix"`
	PublicBaseURL   string `json:"publicBaseURL"`
	PathStyle       bool   `json:"pathStyle"`
}

var (
	ErrConfig    = errors.New("invalid image host configuration")
	ErrUnsafeURL = errors.New("expected a safe HTTPS URL or an explicit loopback HTTP URL")
	ErrLocal     = errors.New("local assets mode does not upload images")
	regionName   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	bucketName   = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
)

func DefaultConfig() Config {
	return Config{Mode: ModeLocal, HTTP: HTTPConfig{Method: http.MethodPost, FileField: "file"}, S3: S3Config{Region: "us-east-1", PathStyle: true}}
}

func (cfg Config) normalized() Config {
	defaults := DefaultConfig()
	if cfg.Mode == "" {
		cfg.Mode = defaults.Mode
	}
	if cfg.HTTP.Method == "" {
		cfg.HTTP.Method = defaults.HTTP.Method
	}
	if cfg.HTTP.FileField == "" {
		cfg.HTTP.FileField = defaults.HTTP.FileField
	}
	if cfg.S3.Region == "" {
		cfg.S3.Region = defaults.S3.Region
	}
	return cfg
}

func (cfg Config) Validate() error {
	cfg = cfg.normalized()
	switch cfg.Mode {
	case ModeLocal:
		return nil
	case ModeHTTP:
		return cfg.HTTP.validate()
	case ModeS3:
		return cfg.S3.validate()
	default:
		return configError("choose Local Assets, Custom HTTP or S3 Compatible")
	}
}

func (cfg HTTPConfig) validate() error {
	if _, err := safeURL(cfg.Endpoint); err != nil {
		return configError("invalid upload endpoint (HTTPS required)")
	}
	if cfg.Method != http.MethodPost && cfg.Method != http.MethodPut {
		return configError("HTTP upload method must be POST or PUT")
	}
	if !utf8.ValidString(cfg.FileField) || strings.ContainsFunc(cfg.FileField, unicode.IsControl) {
		return configError("invalid multipart file field")
	}
	seen := make(map[string]bool, len(cfg.Headers))
	for name, value := range cfg.Headers {
		key := http.CanonicalHeaderKey(name)
		if !headerToken(name) || seen[key] || strings.ContainsFunc(value, func(char rune) bool { return char != '\t' && unicode.IsControl(char) }) {
			return configError("invalid or duplicate custom HTTP header")
		}
		seen[key] = true
		switch key {
		case "Host", "Content-Type", "Content-Length", "Transfer-Encoding", "Connection", "Trailer", "Upgrade", "Expect", "Proxy-Authorization":
			return configError("a custom header is reserved by the upload client")
		}
	}
	if cfg.JSONPath != "" {
		for segment := range strings.SplitSeq(cfg.JSONPath, ".") {
			if strings.TrimSpace(segment) != segment || segment == "" || strings.ContainsFunc(segment, unicode.IsControl) {
				return configError("response JSON path must be dot-separated object keys")
			}
		}
	}
	return nil
}

func (cfg S3Config) validate() error {
	endpoint, err := safeURL(cfg.Endpoint)
	if err != nil || endpoint.ForceQuery || endpoint.RawQuery != "" {
		return configError("invalid S3 endpoint (HTTPS, without a query)")
	}
	if !regionName.MatchString(cfg.Region) || !bucketName.MatchString(cfg.Bucket) || strings.Contains(cfg.Bucket, "..") || net.ParseIP(cfg.Bucket) != nil {
		return configError("invalid S3 region or bucket name")
	}
	if !headerToken(cfg.AccessKeyID) || strings.Contains(cfg.AccessKeyID, "/") || cfg.SecretAccessKey == "" || strings.ContainsFunc(cfg.SecretAccessKey, unicode.IsSpace) || strings.ContainsFunc(cfg.SecretAccessKey, unicode.IsControl) {
		return configError("invalid or missing S3 credentials")
	}
	if !cfg.PathStyle && (endpoint.Scheme != "https" || net.ParseIP(endpoint.Hostname()) != nil) {
		return configError("virtual-hosted S3 requests require an HTTPS DNS hostname")
	}
	if len(cfg.PathPrefix) > 768 || !utf8.ValidString(cfg.PathPrefix) || strings.ContainsFunc(cfg.PathPrefix, unicode.IsControl) || strings.Contains(cfg.PathPrefix, "\\") || !safePath(cfg.PathPrefix) {
		return configError("invalid S3 object path prefix")
	}
	if cfg.PublicBaseURL != "" {
		base, err := safeURL(cfg.PublicBaseURL)
		if err != nil || base.ForceQuery || base.RawQuery != "" || !safePath(base.Path) {
			return configError("invalid S3 public base URL (HTTPS, without a query)")
		}
	}
	if !safePath(endpoint.Path) {
		return configError("invalid S3 endpoint path")
	}
	return nil
}

func configError(message string) error { return fmt.Errorf("%w: %s", ErrConfig, message) }

func headerToken(value string) bool {
	return value != "" && !strings.ContainsFunc(value, func(char rune) bool {
		return !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", char))
	})
}

func safePath(value string) bool {
	for segment := range strings.SplitSeq(value, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func ValidateURL(raw string) error {
	_, err := safeURL(raw)
	return err
}

func safeURL(raw string) (*url.URL, error) {
	if raw == "" || !utf8.ValidString(raw) || strings.ContainsAny(raw, "<>\\#") || strings.ContainsFunc(raw, func(char rune) bool { return unicode.IsSpace(char) || unicode.IsControl(char) }) {
		return nil, ErrUnsafeURL
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Hostname() == "" || parsed.Opaque != "" {
		return nil, ErrUnsafeURL
	}
	host := parsed.Hostname()
	if strings.HasPrefix(parsed.Host, "[") {
		if !strings.Contains(host, ":") || net.ParseIP(host) == nil {
			return nil, ErrUnsafeURL
		}
	} else if strings.ContainsAny(host, ":%") {
		return nil, ErrUnsafeURL
	}
	if _, err := url.QueryUnescape(parsed.RawQuery); err != nil {
		return nil, ErrUnsafeURL
	}
	if parsed.Scheme == "https" || parsed.Scheme == "http" && explicitLoopback(host) {
		return parsed, nil
	}
	return nil, ErrUnsafeURL
}

func explicitLoopback(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
