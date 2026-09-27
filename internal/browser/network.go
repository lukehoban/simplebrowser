package browser

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultNetworkTimeout = 15 * time.Second
	defaultMaxRedirects   = 10
	defaultMaxBodyBytes   = 16 << 20
	defaultMaxHeaderBytes = 64 << 10
	maxProtocolLineBytes  = 8 << 10
)

// Fetcher loads HTTP(S), file://, and local-path resources. Its fields are
// optional; zero values select conservative defaults. DialContext and
// TLSConfig provide seams for deterministic tests and embedding applications.
type Fetcher struct {
	DialContext    func(context.Context, string, string) (net.Conn, error)
	TLSConfig      *tls.Config
	Timeout        time.Duration
	MaxRedirects   int
	MaxBodyBytes   int64
	MaxHeaderBytes int
}

// Fetch loads source, following a bounded number of HTTP redirects.
func (f *Fetcher) Fetch(source string) (Resource, error) {
	return f.FetchContext(context.Background(), source)
}

// FetchContext loads source while respecting the caller's cancellation and
// deadline. HTTP response bodies are fully buffered up to MaxBodyBytes.
func (f *Fetcher) FetchContext(parent context.Context, source string) (Resource, error) {
	if parent == nil {
		parent = context.Background()
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return Resource{}, fmt.Errorf("source must not be empty")
	}

	if isLocalPath(source) {
		if strings.HasPrefix(strings.ToLower(source), "file:") {
			target, err := url.Parse(source)
			if err != nil {
				return Resource{}, fmt.Errorf("parse file URL: %w", err)
			}
			if (target.Host != "" && !strings.EqualFold(target.Host, "localhost")) || target.RawQuery != "" || target.Fragment != "" || target.Path == "" {
				return Resource{}, fmt.Errorf("file URL must refer to a local path without query or fragment")
			}
		}
		body, err := f.readFile(localPath(source))
		if err != nil {
			return Resource{}, err
		}
		return Resource{Source: source, Body: body}, nil
	}

	timeout := f.Timeout
	if timeout <= 0 {
		timeout = defaultNetworkTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	current, err := parseHTTPURL(source)
	if err != nil {
		return Resource{}, err
	}
	maxRedirects := f.MaxRedirects
	if maxRedirects <= 0 {
		maxRedirects = defaultMaxRedirects
	}
	for redirects := 0; ; redirects++ {
		body, headers, status, err := f.request(ctx, current)
		if err != nil {
			return Resource{}, err
		}
		if isRedirectStatus(status) {
			location := headers.Get("Location")
			if location != "" {
				if redirects >= maxRedirects {
					return Resource{}, fmt.Errorf("too many redirects (limit %d)", maxRedirects)
				}
				reference, err := url.Parse(location)
				if err != nil {
					return Resource{}, fmt.Errorf("invalid redirect location: %w", err)
				}
				current = current.ResolveReference(reference)
				if _, err := parseHTTPURL(current.String()); err != nil {
					return Resource{}, fmt.Errorf("invalid redirect target: %w", err)
				}
				continue
			}
		}
		if status < 200 || status >= 300 {
			return Resource{}, fmt.Errorf("HTTP %d %s", status, statusText(status))
		}
		return Resource{Source: source, Body: body}, nil
	}
}

func (f *Fetcher) request(ctx context.Context, target *url.URL) ([]byte, textproto.MIMEHeader, int, error) {
	host := target.Hostname()
	port := target.Port()
	if port == "" {
		if target.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	address := net.JoinHostPort(host, port)
	dial := f.DialContext
	if dial == nil {
		dialer := &net.Dialer{}
		dial = dialer.DialContext
	}
	conn, err := dial(ctx, "tcp", address)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("connect to %s: %w", address, err)
	}
	rawConn := conn
	defer conn.Close()
	stopCancel := context.AfterFunc(ctx, func() {
		_ = rawConn.SetDeadline(time.Now())
	})
	defer stopCancel()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, nil, 0, fmt.Errorf("set connection deadline: %w", err)
		}
	}

	if target.Scheme == "https" {
		config := &tls.Config{MinVersion: tls.VersionTLS12}
		if f.TLSConfig != nil {
			config = f.TLSConfig.Clone()
			if config.MinVersion == 0 {
				config.MinVersion = tls.VersionTLS12
			}
		}
		if config.ServerName == "" {
			config.ServerName = host
		}
		tlsConn := tls.Client(conn, config)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return nil, nil, 0, fmt.Errorf("TLS handshake: %w", err)
		}
		conn = tlsConn
	}

	requestURI := target.RequestURI()
	if requestURI == "" {
		requestURI = "/"
	}
	request := "GET " + requestURI + " HTTP/1.1\r\n" +
		"Host: " + target.Host + "\r\n" +
		"User-Agent: simplebrowser/0.1\r\n" +
		"Accept: */*\r\n" +
		"Accept-Encoding: gzip\r\n" +
		"Connection: close\r\n\r\n"
	if _, err := io.WriteString(conn, request); err != nil {
		return nil, nil, 0, fmt.Errorf("write HTTP request: %w", err)
	}

	reader := bufio.NewReaderSize(conn, 4096)
	maxHeaderBytes := f.MaxHeaderBytes
	if maxHeaderBytes <= 0 {
		maxHeaderBytes = defaultMaxHeaderBytes
	}
	var status int
	var headers textproto.MIMEHeader
	for informational := 0; ; informational++ {
		status, headers, err = readResponseHeader(reader, maxHeaderBytes)
		if err != nil {
			return nil, nil, 0, err
		}
		if status < 100 || status >= 200 || status == 101 {
			break
		}
		if informational >= 5 {
			return nil, nil, 0, fmt.Errorf("too many informational HTTP responses")
		}
	}

	maxBodyBytes := f.bodyLimit()
	body := []byte(nil)
	if status != 204 && status != 304 && status/100 != 1 {
		body, err = readHTTPBody(reader, headers, maxBodyBytes, maxHeaderBytes)
		if err != nil {
			return nil, nil, 0, err
		}
	} else {
		return body, headers, status, nil
	}
	decoded, err := decodeContent(body, headers.Get("Content-Encoding"), maxBodyBytes)
	if err != nil {
		return nil, nil, 0, err
	}
	return decoded, headers, status, nil
}

func (f *Fetcher) bodyLimit() int64 {
	if f.MaxBodyBytes <= 0 {
		return defaultMaxBodyBytes
	}
	return f.MaxBodyBytes
}

func (f *Fetcher) readFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open local file %q: %w", path, err)
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, f.bodyLimit()+1))
	if err != nil {
		return nil, fmt.Errorf("read local file %q: %w", path, err)
	}
	if int64(len(body)) > f.bodyLimit() {
		return nil, fmt.Errorf("local file exceeds body limit of %d bytes", f.bodyLimit())
	}
	return body, nil
}

func parseHTTPURL(source string) (*url.URL, error) {
	target, err := url.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	target.Scheme = strings.ToLower(target.Scheme)
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme %q", target.Scheme)
	}
	if target.Host == "" || target.Hostname() == "" {
		return nil, fmt.Errorf("URL must include a hostname")
	}
	if target.User != nil {
		return nil, fmt.Errorf("URL user information is not supported")
	}
	if target.Opaque != "" {
		return nil, fmt.Errorf("opaque URLs are not supported")
	}
	if target.Port() != "" {
		port, err := strconv.Atoi(target.Port())
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid URL port %q", target.Port())
		}
	}
	target.Fragment = ""
	return target, nil
}

func isLocalPath(source string) bool {
	target, err := url.Parse(source)
	if err != nil {
		return !strings.Contains(source, "://")
	}
	return target.Scheme == "" || strings.EqualFold(target.Scheme, "file")
}

func localPath(source string) string {
	target, err := url.Parse(source)
	if err != nil || target.Scheme == "" {
		return source
	}
	if !strings.EqualFold(target.Scheme, "file") {
		return source
	}
	if target.Host != "" && !strings.EqualFold(target.Host, "localhost") {
		return ""
	}
	return target.Path
}

func readResponseHeader(reader *bufio.Reader, limit int) (int, textproto.MIMEHeader, error) {
	var total int
	statusLine, size, err := readBoundedLine(reader, limit)
	if err != nil {
		return 0, nil, fmt.Errorf("read HTTP status line: %w", err)
	}
	total += size
	parts := strings.Split(strings.TrimSuffix(statusLine, "\r\n"), " ")
	if len(parts) < 2 || (parts[0] != "HTTP/1.0" && parts[0] != "HTTP/1.1") || len(parts[1]) != 3 {
		return 0, nil, fmt.Errorf("malformed HTTP status line")
	}
	for _, digit := range parts[1] {
		if digit < '0' || digit > '9' {
			return 0, nil, fmt.Errorf("malformed HTTP status code")
		}
	}
	status, _ := strconv.Atoi(parts[1])
	if status < 100 || status > 999 {
		return 0, nil, fmt.Errorf("invalid HTTP status code %d", status)
	}

	var raw bytes.Buffer
	for {
		line, size, err := readBoundedLine(reader, limit-total)
		if err != nil {
			return 0, nil, fmt.Errorf("read HTTP headers: %w", err)
		}
		total += size
		if total > limit {
			return 0, nil, fmt.Errorf("HTTP headers exceed limit of %d bytes", limit)
		}
		raw.WriteString(line)
		if line == "\r\n" {
			break
		}
	}
	headerReader := textproto.NewReader(bufio.NewReader(bytes.NewReader(raw.Bytes())))
	headers, err := headerReader.ReadMIMEHeader()
	if err != nil {
		return 0, nil, fmt.Errorf("parse HTTP headers: %w", err)
	}
	return status, headers, nil
}

func readBoundedLine(reader *bufio.Reader, remaining int) (string, int, error) {
	if remaining <= 0 {
		return "", 0, fmt.Errorf("line exceeds size limit")
	}
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > remaining || len(line) > maxProtocolLineBytes {
			return "", len(line), fmt.Errorf("line exceeds size limit")
		}
		if err == nil {
			if len(line) < 2 || line[len(line)-2] != '\r' {
				return "", len(line), fmt.Errorf("line is not terminated with CRLF")
			}
			return string(line), len(line), nil
		}
		if err != bufio.ErrBufferFull {
			return "", len(line), err
		}
	}
}

func readHTTPBody(reader *bufio.Reader, headers textproto.MIMEHeader, limit int64, headerLimit int) ([]byte, error) {
	transferEncodings := commaValues(headers.Values("Transfer-Encoding"))
	lengths := commaValues(headers.Values("Content-Length"))
	if len(transferEncodings) > 0 && len(lengths) > 0 {
		return nil, fmt.Errorf("response contains both Transfer-Encoding and Content-Length")
	}
	if len(transferEncodings) > 0 {
		for i := range transferEncodings {
			transferEncodings[i] = strings.ToLower(strings.TrimSpace(transferEncodings[i]))
		}
		if len(transferEncodings) != 1 || transferEncodings[0] != "chunked" {
			return nil, fmt.Errorf("unsupported Transfer-Encoding %q", strings.Join(transferEncodings, ", "))
		}
		return readChunkedBody(reader, limit, headerLimit)
	}
	if len(lengths) > 0 {
		var expected int64 = -1
		for _, value := range lengths {
			n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid Content-Length %q", value)
			}
			if expected >= 0 && n != expected {
				return nil, fmt.Errorf("conflicting Content-Length headers")
			}
			expected = n
		}
		if expected > limit {
			return nil, fmt.Errorf("response body exceeds limit of %d bytes", limit)
		}
		body := make([]byte, expected)
		if _, err := io.ReadFull(reader, body); err != nil {
			return nil, fmt.Errorf("read response body: %w", err)
		}
		return body, nil
	}
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read connection-close response body: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response body exceeds limit of %d bytes", limit)
	}
	return body, nil
}

func readChunkedBody(reader *bufio.Reader, limit int64, trailerLimit int) ([]byte, error) {
	var body bytes.Buffer
	for {
		line, _, err := readBoundedLine(reader, maxProtocolLineBytes)
		if err != nil {
			return nil, fmt.Errorf("read chunk size: %w", err)
		}
		sizeText := strings.TrimSuffix(line, "\r\n")
		if extension := strings.IndexByte(sizeText, ';'); extension >= 0 {
			sizeText = sizeText[:extension]
		}
		if sizeText == "" {
			return nil, fmt.Errorf("empty chunk size")
		}
		size, err := strconv.ParseUint(sizeText, 16, 63)
		if err != nil {
			return nil, fmt.Errorf("invalid chunk size %q", sizeText)
		}
		if size == 0 {
			if err := readTrailers(reader, trailerLimit); err != nil {
				return nil, err
			}
			return body.Bytes(), nil
		}
		if size > uint64(limit-int64(body.Len())) {
			return nil, fmt.Errorf("response body exceeds limit of %d bytes", limit)
		}
		start := body.Len()
		body.Grow(int(size))
		if _, err := io.CopyN(&body, reader, int64(size)); err != nil {
			return nil, fmt.Errorf("read chunk data: %w", err)
		}
		if body.Len()-start != int(size) {
			return nil, fmt.Errorf("incomplete chunk data")
		}
		ending := make([]byte, 2)
		if _, err := io.ReadFull(reader, ending); err != nil || string(ending) != "\r\n" {
			return nil, fmt.Errorf("malformed chunk terminator")
		}
	}
}

func readTrailers(reader *bufio.Reader, limit int) error {
	var raw bytes.Buffer
	total := 0
	for {
		line, size, err := readBoundedLine(reader, limit-total)
		if err != nil {
			return fmt.Errorf("read chunk trailers: %w", err)
		}
		total += size
		raw.WriteString(line)
		if line == "\r\n" {
			break
		}
	}
	if _, err := textproto.NewReader(bufio.NewReader(bytes.NewReader(raw.Bytes()))).ReadMIMEHeader(); err != nil {
		return fmt.Errorf("parse chunk trailers: %w", err)
	}
	return nil
}

func decodeContent(body []byte, encoding string, limit int64) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "identity":
		return body, nil
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("decode gzip response: %w", err)
		}
		defer reader.Close()
		decoded, err := io.ReadAll(io.LimitReader(reader, limit+1))
		if err != nil {
			return nil, fmt.Errorf("read gzip response: %w", err)
		}
		if int64(len(decoded)) > limit {
			return nil, fmt.Errorf("decompressed response exceeds limit of %d bytes", limit)
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("unsupported Content-Encoding %q", encoding)
	}
}

func commaValues(values []string) []string {
	var result []string
	for _, value := range values {
		result = append(result, strings.Split(value, ",")...)
	}
	return result
}

func isRedirectStatus(status int) bool {
	switch status {
	case 301, 302, 303, 307, 308:
		return true
	default:
		return false
	}
}

func statusText(status int) string {
	switch status {
	case 301:
		return "Moved Permanently"
	case 302:
		return "Found"
	case 303:
		return "See Other"
	case 307:
		return "Temporary Redirect"
	case 308:
		return "Permanent Redirect"
	default:
		return "response"
	}
}
