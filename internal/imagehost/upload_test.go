package imagehost

import (
	"context"
	"errors"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (transport roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func TestHTTPUploadMultipart(t *testing.T) {
	cfg := Config{Mode: ModeHTTP, HTTP: HTTPConfig{
		Endpoint: "https://upload.example/upload", Method: "PUT", FileField: "photo",
		Headers: map[string]string{"Authorization": "Bearer private-token", "X-Version": "1"}, JSONPath: "data.url",
	}}
	uploader := uploader{transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != "PUT" || request.URL.String() != cfg.HTTP.Endpoint || request.Header.Get("Authorization") != "Bearer private-token" {
			t.Error("wrong request method, endpoint or authorization")
		}
		media, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || media != "multipart/form-data" {
			t.Fatal("expected multipart/form-data")
		}
		reader := multipart.NewReader(request.Body, params["boundary"])
		part, err := reader.NextPart()
		if err != nil || part.FormName() != "photo" || part.FileName() != "照片.png" || part.Header.Get("Content-Type") != "image/png" || strings.Contains(part.Header.Get("Content-Disposition"), "filename*") {
			t.Fatal("incorrect multipart part")
		}
		if body, err := io.ReadAll(part); err != nil || string(body) != "png" {
			t.Fatal("wrong multipart data")
		}
		if _, err := reader.NextPart(); err != io.EOF {
			t.Fatal("unexpected multipart content")
		}
		return reply(201, `{"data":{"url":"https://cdn.example/p1.png"}}`), nil
	})}
	if url, err := uploader.upload(t.Context(), cfg, "照片.png", []byte("png"), "image/png"); err != nil || url != "https://cdn.example/p1.png" {
		t.Fatalf("upload: url=%q err=%v", url, err)
	}
}

func TestHTTPUploadResponseSafety(t *testing.T) {
	const authToken = "private" + "-token"
	for _, check := range []struct{ name, endpoint, path, response, want string }{
		{"plain", "https://upload.example", "", " https://cdn.example/p.png\n", "https://cdn.example/p.png"},
		{"loopback IPv6", "http://[::1]:8080", "", "http://[::1]:8080/p.png", "http://[::1]:8080/p.png"},
		{"loopback IPv4", "http://127.0.0.1", "url", `{"url":"http://127.0.0.1/p.png"}`, "http://127.0.0.1/p.png"},
		{"external HTTP endpoint", "http://upload.example", "", "https://cdn.example/p.png", ""},
		{"non-explicit loopback", "http://127.0.0.2", "", "https://cdn.example/p.png", ""},
		{"external HTTP response", "https://upload.example", "", "http://cdn.example/p.png", ""},
		{"userinfo", "https://upload.example", "", "https://username:password@cdn.example/p.png", ""},
		{"Markdown injection", "https://upload.example", "", "https://cdn.example/p.png>)[x](https://example.net)", ""},
		{"Markdown whitespace", "https://upload.example", "", "https://cdn.example/p.png\nhello", ""},
		{"unsafe scheme", "https://upload.example", "", "javascript:alert(1)", ""},
		{"invalid bracket host", "https://upload.example", "", "http://[localhost]:8080/p.png", ""},
		{"invalid query escape", "https://upload.example", "", "https://cdn.example/?bad=%ZZ", ""},
		{"relative", "https://upload.example", "", "//cdn.example/p.png", ""},
		{"missing path", "https://upload.example", "data.url", `{"data":{"other":"https://cdn.example"}}`, ""},
		{"non-string path", "https://upload.example", "data.url", `{"data":{"url":123}}`, ""},
		{"credential in URL", "https://upload.example", "url", `{"url":"https://cdn.example/?echo=` + authToken + `"}`, ""},
	} {
		t.Run(check.name, func(t *testing.T) {
			cfg := Config{Mode: ModeHTTP, HTTP: HTTPConfig{Endpoint: check.endpoint, JSONPath: check.path, Headers: map[string]string{"Authorization": "Bearer " + authToken}}}
			uploader := uploader{transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.Method != "POST" {
					t.Error("expected default POST")
				}
				return reply(200, check.response), nil
			})}
			url, err := uploader.upload(t.Context(), cfg, "p.png", []byte("png"), "")
			if url != check.want || (err != nil) != (check.want == "") {
				t.Fatalf("upload: url=%q err=%v", url, err)
			}
		})
	}
}

func TestConnectionUploadsTinyPNG(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = ModeS3
	cfg.S3.Endpoint, cfg.S3.Bucket = "https://store.example", "bucket"
	cfg.S3.AccessKeyID, cfg.S3.SecretAccessKey = "test-id", "test-key"
	sender := uploader{transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		bitmap, err := png.Decode(request.Body)
		if err != nil || bitmap.Bounds().Dx() != 1 || bitmap.Bounds().Dy() != 1 || !strings.HasSuffix(request.URL.Path, "-markstack-upload-test.png") {
			t.Fatal("connection test did not send a tiny PNG with a reserved filename")
		}
		if _, ok := request.Context().Deadline(); !ok {
			t.Error("upload request has no deadline")
		}
		return reply(200, ""), nil
	})}
	if err := sender.testConnection(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
}

func TestUploadRedactsAndDoesNotFollowRedirects(t *testing.T) {
	cfg := Config{Mode: ModeHTTP, HTTP: HTTPConfig{Endpoint: "https://upload.example/?token=endpoint-secret", Headers: map[string]string{"Authorization": "Bearer header-secret"}}}
	for _, check := range []struct {
		name string
		send roundTripFunc
		want error
	}{
		{"transport", func(*http.Request) (*http.Response, error) { return nil, errors.New("endpoint-secret header-secret") }, ErrNetwork},
		{"timeout", func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded }, ErrTimeout},
		{"response", func(*http.Request) (*http.Response, error) {
			response := reply(403, "endpoint-secret header-secret")
			response.Status = "403 header-secret"
			return response, nil
		}, ErrStatus},
		{"redirect", func(request *http.Request) (*http.Response, error) {
			if request.URL.Host != "upload.example" {
				t.Fatal("unexpected redirected request")
			}
			response := reply(307, "")
			response.Header = http.Header{"Location": {"https://other.example/header-secret"}}
			return response, nil
		}, ErrStatus},
		{"large response", func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("a", 65537)), nil }, ErrResponse},
	} {
		t.Run(check.name, func(t *testing.T) {
			_, err := (uploader{transport: check.send}).upload(t.Context(), cfg, "p.png", []byte("png"), "")
			if !errors.Is(err, check.want) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("wrong or unredacted error: %v", err)
			}
		})
	}
	if _, err := Upload(t.Context(), cfg, "p.png", make([]byte, MaxUploadBytes+1), ""); !errors.Is(err, ErrSize) {
		t.Fatalf("oversized upload: %v", err)
	}
}

func TestS3CanonicalRequest(t *testing.T) {
	request, err := http.NewRequest("PUT", "https://examplebucket.s3.amazonaws.com/test%20file.png", strings.NewReader("png"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "image/png")
	when := time.Date(2026, 11, 28, 12, 34, 56, 0, time.UTC)
	const payload = "8f8cbb7dcf46e0bc7d53265749a6c17d116093a6ba95e442764060c76fd4a86c"
	canonical := signS3(request, S3Config{Region: "us-east-1", AccessKeyID: "example-key", SecretAccessKey: "example-secret"}, payload, when)
	want := "PUT\n/test%20file.png\n\ncontent-type:image/png\nhost:examplebucket.s3.amazonaws.com\nx-amz-content-sha256:" + payload + "\nx-amz-date:20261128T123456Z\n\ncontent-type;host;x-amz-content-sha256;x-amz-date\n" + payload
	if canonical != want {
		t.Fatalf("canonical request:\n%s", canonical)
	}
	const signature = "AWS4-HMAC-SHA256 " + "Credential=" + "example-key" + "/20261128/us-east-1/s3/aws4_request, SignedHeaders=content-type;host;x-amz-content-sha256;x-amz-date, Signature=21033b7161c82468761884e530ef4b7ff59de8f46a7071b413cab7e807f7a87f"
	if got := request.Header.Get("Authorization"); got != signature {
		t.Errorf("signature = %s", got)
	}
}

func TestS3UploadURLs(t *testing.T) {
	for _, check := range []struct {
		name, endpoint, base, wantUpload, wantPublic string
		pathStyle                                    bool
	}{
		{"path", "https://s3.example:8443/proxy", "", "https://s3.example:8443/proxy/bucket/notes%20%2B%20pics/", "https://s3.example:8443/proxy/bucket/notes%20%2B%20pics/", true},
		{"virtual and CDN", "https://s3.example", "https://cdn.example/files/", "https://bucket.s3.example/notes%20%2B%20pics/", "https://cdn.example/files/notes%20%2B%20pics/", false},
	} {
		t.Run(check.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Mode = ModeS3
			cfg.S3.Endpoint, cfg.S3.PublicBaseURL, cfg.S3.PathStyle = check.endpoint, check.base, check.pathStyle
			cfg.S3.Bucket, cfg.S3.AccessKeyID, cfg.S3.SecretAccessKey = "bucket", "key", "secret"
			cfg.S3.PathPrefix = "notes + pics/"
			var object string
			uploader := uploader{transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				got := request.URL.String()
				if request.Method != "PUT" || !strings.HasPrefix(got, check.wantUpload) || !strings.HasSuffix(got, "-p%20%281%29.png") {
					t.Fatalf("unexpected upload URL: %s", got)
				}
				object = strings.TrimPrefix(got, check.wantUpload)
				body, err := io.ReadAll(request.Body)
				if err != nil || string(body) != "png" || request.Header.Get("x-amz-content-sha256") != sha256Hex(body) || request.Header.Get("Authorization") == "" {
					t.Fatal("incorrect signed PUT")
				}
				return reply(200, ""), nil
			})}
			if remote, err := uploader.upload(t.Context(), cfg, "p (1).png", []byte("png"), "image/png"); err != nil || remote != check.wantPublic+object {
				t.Fatalf("public URL: %q, %v", remote, err)
			}
		})
	}
}
