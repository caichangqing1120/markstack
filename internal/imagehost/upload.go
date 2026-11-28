package imagehost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxUploadBytes = 20 << 20
	maxReplyBytes  = 64 << 10
	uploadTimeout  = 30 * time.Second
)

var (
	ErrSize     = errors.New("image must be between 1 byte and 20 MiB")
	ErrImage    = errors.New("invalid image filename or content type")
	ErrTimeout  = errors.New("image upload timed out")
	ErrCanceled = errors.New("image upload canceled")
	ErrNetwork  = errors.New("could not reach the image host; check the endpoint and network")
	ErrStatus   = errors.New("image host rejected the upload")
	ErrResponse = errors.New("image host response did not contain a usable URL")
	formQuotes  = strings.NewReplacer("\\", "\\\\", `"`, `\"`)
)

type StatusError int

func (status StatusError) Error() string { return fmt.Sprintf("%s (HTTP %d)", ErrStatus, status) }
func (status StatusError) Unwrap() error { return ErrStatus }

type uploader struct {
	transport http.RoundTripper
}

func Upload(ctx context.Context, cfg Config, filename string, data []byte, contentType string) (string, error) {
	return (uploader{}).upload(ctx, cfg, filename, data, contentType)
}

func (sender uploader) upload(ctx context.Context, cfg Config, filename string, data []byte, contentType string) (string, error) {
	if len(data) == 0 || len(data) > MaxUploadBytes {
		return "", ErrSize
	}
	cfg = cfg.normalized()
	if err := cfg.Validate(); err != nil {
		return "", err
	}
	if cfg.Mode == ModeLocal {
		return "", ErrLocal
	}
	filename = path.Base(strings.ReplaceAll(filename, "\\", "/"))
	if filename == "." || filename == ".." || filename == "/" || len(filename) > 180 || !utf8.ValidString(filename) || strings.ContainsFunc(filename, unicode.IsControl) {
		return "", ErrImage
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.Contains(mediaType, "/") || strings.ContainsFunc(contentType, unicode.IsControl) {
		return "", ErrImage
	}
	contentType = mime.FormatMediaType(mediaType, params)
	ctx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return "", networkError(err)
	}
	var request *http.Request
	var publicURL string
	if cfg.Mode == ModeHTTP {
		request, err = multipartRequest(ctx, cfg.HTTP, filename, data, contentType)
	} else {
		request, publicURL, err = s3Request(ctx, cfg.S3, filename, data, contentType, time.Now())
	}
	if err != nil {
		return "", err
	}
	client := http.Client{Transport: sender.transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return "", networkError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", StatusError(response.StatusCode)
	}
	if cfg.Mode == ModeS3 {
		return publicURL, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxReplyBytes+1))
	if err != nil {
		return "", networkError(err)
	}
	if len(body) > maxReplyBytes {
		return "", ErrResponse
	}
	return responseURL(cfg.HTTP, body)
}

func TestConnection(ctx context.Context, cfg Config) error {
	return (uploader{}).testConnection(ctx, cfg)
}

func (sender uploader) testConnection(ctx context.Context, cfg Config) error {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		return ErrImage
	}
	_, err := sender.upload(ctx, cfg, "markstack-upload-test.png", data.Bytes(), "image/png")
	return err
}

func networkError(err error) error {
	var network net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &network) && network.Timeout():
		return ErrTimeout
	case errors.Is(err, context.Canceled):
		return ErrCanceled
	default:
		return ErrNetwork
	}
}

func multipartRequest(ctx context.Context, cfg HTTPConfig, filename string, data []byte, contentType string) (*http.Request, error) {
	var body bytes.Buffer
	body.Grow(len(data) + 512)
	form := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="`+formQuotes.Replace(cfg.FileField)+`"; filename="`+formQuotes.Replace(filename)+`"`)
	header.Set("Content-Type", contentType)
	part, err := form.CreatePart(header)
	if err != nil {
		return nil, ErrImage
	}
	if _, err := part.Write(data); err != nil {
		return nil, ErrImage
	}
	if err := form.Close(); err != nil {
		return nil, ErrImage
	}
	request, err := http.NewRequestWithContext(ctx, cfg.Method, cfg.Endpoint, &body)
	if err != nil {
		return nil, ErrConfig
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	for key, value := range cfg.Headers {
		request.Header.Set(key, strings.TrimSpace(value))
	}
	return request, nil
}

func responseURL(cfg HTTPConfig, body []byte) (string, error) {
	value := strings.TrimSpace(string(body))
	if cfg.JSONPath != "" {
		var current any
		if err := json.Unmarshal(body, &current); err != nil {
			return "", ErrResponse
		}
		for key := range strings.SplitSeq(cfg.JSONPath, ".") {
			object, ok := current.(map[string]any)
			if !ok {
				return "", ErrResponse
			}
			current = object[key]
		}
		value, _ = current.(string)
	}
	if err := ValidateURL(value); err != nil {
		return "", err
	}
	decodedPath, _ := url.PathUnescape(value)
	decodedQuery, _ := url.QueryUnescape(value)
	for key, header := range cfg.Headers {
		key = strings.ToLower(key)
		header = strings.TrimSpace(header)
		switch {
		case key == "authorization":
			if _, token, ok := strings.Cut(header, " "); ok {
				header = strings.TrimSpace(token)
			}
		case key == "cookie", strings.Contains(key, "token"), strings.Contains(key, "secret"), strings.Contains(key, "key"):
		default:
			continue
		}
		if header != "" && (strings.Contains(value, header) || strings.Contains(decodedPath, header) || strings.Contains(decodedQuery, header)) {
			return "", ErrResponse
		}
	}
	return value, nil
}
