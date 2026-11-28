package imagehost

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func s3Request(ctx context.Context, cfg S3Config, filename string, data []byte, contentType string, when time.Time) (*http.Request, string, error) {
	endpoint, _ := url.Parse(cfg.Endpoint)
	payload := sha256Hex(data)
	object := payload + "-" + filename
	if prefix := strings.Trim(cfg.PathPrefix, "/"); prefix != "" {
		object = prefix + "/" + object
	}
	if cfg.PathStyle {
		appendS3Path(endpoint, cfg.Bucket+"/"+object)
	} else {
		host := cfg.Bucket + "." + endpoint.Hostname()
		if port := endpoint.Port(); port != "" {
			host = net.JoinHostPort(host, port)
		}
		endpoint.Host = host
		appendS3Path(endpoint, object)
	}
	publicURL := endpoint.String()
	if cfg.PublicBaseURL != "" {
		base, _ := url.Parse(cfg.PublicBaseURL)
		appendS3Path(base, object)
		publicURL = base.String()
	}
	if err := ValidateURL(publicURL); err != nil {
		return nil, "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint.String(), bytes.NewReader(data))
	if err != nil {
		return nil, "", ErrConfig
	}
	request.Header.Set("Content-Type", contentType)
	signS3(request, cfg, payload, when)
	return request, publicURL, nil
}

func appendS3Path(base *url.URL, suffix string) {
	base.Path = strings.TrimRight(base.Path, "/") + "/" + suffix
	base.RawPath = s3EscapePath(base.Path)
}

func s3EscapePath(value string) string {
	const hexDigits = "0123456789ABCDEF"
	var escaped strings.Builder
	escaped.Grow(len(value))
	for index := 0; index < len(value); index++ {
		char := value[index]
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("-._~/", rune(char)) {
			escaped.WriteByte(char)
		} else {
			escaped.WriteByte('%')
			escaped.WriteByte(hexDigits[char>>4])
			escaped.WriteByte(hexDigits[char&15])
		}
	}
	return escaped.String()
}

func signS3(request *http.Request, cfg S3Config, payload string, when time.Time) string {
	const algorithm = "AWS4-HMAC-SHA256"
	const signedHeaders = "content-type;host;x-amz-content-sha256;x-amz-date"
	stamp := when.UTC().Format("20060102T150405Z")
	day := stamp[:8]
	request.Header.Set("X-Amz-Date", stamp)
	request.Header.Set("X-Amz-Content-Sha256", payload)
	contentType := strings.Join(strings.Fields(request.Header.Get("Content-Type")), " ")
	canonical := request.Method + "\n" + request.URL.EscapedPath() + "\n\n" +
		"content-type:" + contentType + "\nhost:" + request.Host + "\nx-amz-content-sha256:" + payload + "\nx-amz-date:" + stamp + "\n\n" + signedHeaders + "\n" + payload
	scope := day + "/" + cfg.Region + "/s3/aws4_request"
	toSign := algorithm + "\n" + stamp + "\n" + scope + "\n" + sha256Hex([]byte(canonical))
	key := []byte("AWS4" + cfg.SecretAccessKey)
	for segment := range strings.SplitSeq(scope, "/") {
		key = hmacSHA256(key, segment)
	}
	signature := hex.EncodeToString(hmacSHA256(key, toSign))
	request.Header.Set("Authorization", algorithm+" Credential="+cfg.AccessKeyID+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
	return canonical
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}
