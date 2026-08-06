package assets

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultRemoteFileOverallTimeout        = 30 * time.Second
	defaultRemoteFileConnectTimeout        = 5 * time.Second
	defaultRemoteFileTLSHandshakeTimeout   = 5 * time.Second
	defaultRemoteFileResponseHeaderTimeout = 10 * time.Second
	defaultRemoteFileMaxRedirects          = 5
)

type RemoteFileFetcher interface {
	Fetch(ctx context.Context, downloadURL string, maxBytes int64) ([]byte, error)
}

type ipResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type dialContextFunc func(ctx context.Context, network string, address string) (net.Conn, error)

type SecureRemoteFileFetcher struct {
	resolver              ipResolver
	dialContext           dialContextFunc
	overallTimeout        time.Duration
	connectTimeout        time.Duration
	tlsHandshakeTimeout   time.Duration
	responseHeaderTimeout time.Duration
	maxRedirects          int
}

func NewSecureRemoteFileFetcher() *SecureRemoteFileFetcher {
	dialer := &net.Dialer{Timeout: defaultRemoteFileConnectTimeout, KeepAlive: -1}
	return &SecureRemoteFileFetcher{
		resolver:              net.DefaultResolver,
		dialContext:           dialer.DialContext,
		overallTimeout:        defaultRemoteFileOverallTimeout,
		connectTimeout:        defaultRemoteFileConnectTimeout,
		tlsHandshakeTimeout:   defaultRemoteFileTLSHandshakeTimeout,
		responseHeaderTimeout: defaultRemoteFileResponseHeaderTimeout,
		maxRedirects:          defaultRemoteFileMaxRedirects,
	}
}

func (f *SecureRemoteFileFetcher) Fetch(ctx context.Context, downloadURL string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, errors.New("ChatGPT file download rejected: maximum file size must be positive")
	}
	parsed, err := url.Parse(strings.TrimSpace(downloadURL))
	if err != nil {
		return nil, fmt.Errorf("ChatGPT file download rejected: invalid URL: %w", err)
	}

	timeout := f.overallTimeout
	if timeout <= 0 {
		timeout = defaultRemoteFileOverallTimeout
	}
	requestContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	state := &secureRemoteFetchState{
		fetcher:  f.withDefaults(),
		expected: map[string][]netip.Addr{},
	}
	if err := state.validateAndPin(requestContext, parsed); err != nil {
		return nil, fmt.Errorf("ChatGPT file download rejected: %w", err)
	}

	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            state.dialContext,
		ForceAttemptHTTP2:      true,
		DisableKeepAlives:      true,
		TLSHandshakeTimeout:    state.fetcher.tlsHandshakeTimeout,
		ResponseHeaderTimeout:  state.fetcher.responseHeaderTimeout,
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: 1 << 20,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= state.fetcher.maxRedirects {
				return errors.New("too many redirects")
			}
			request.Header = make(http.Header)
			request.Host = ""
			if err := state.validateAndPin(request.Context(), request.URL); err != nil {
				return fmt.Errorf("unsafe redirect destination: %w", err)
			}
			return nil
		},
	}

	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("ChatGPT file download rejected: %w", err)
	}
	request.Header = make(http.Header)

	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestContext.Err(), context.DeadlineExceeded) {
			return nil, errors.New("ChatGPT file download timed out")
		}
		return nil, fmt.Errorf("ChatGPT file download failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("ChatGPT file download failed with HTTP status %d", response.StatusCode)
	}
	if response.ContentLength > maxBytes {
		return nil, fmt.Errorf("File is too large. Max size is %d bytes.", maxBytes)
	}

	contents, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestContext.Err(), context.DeadlineExceeded) {
			return nil, errors.New("ChatGPT file download timed out")
		}
		return nil, fmt.Errorf("ChatGPT file download failed: %w", err)
	}
	if int64(len(contents)) > maxBytes {
		return nil, fmt.Errorf("File is too large. Max size is %d bytes.", maxBytes)
	}
	return contents, nil
}

func (f *SecureRemoteFileFetcher) withDefaults() *SecureRemoteFileFetcher {
	result := *f
	if result.resolver == nil {
		result.resolver = net.DefaultResolver
	}
	if result.dialContext == nil {
		dialer := &net.Dialer{Timeout: defaultRemoteFileConnectTimeout, KeepAlive: -1}
		result.dialContext = dialer.DialContext
	}
	if result.connectTimeout <= 0 {
		result.connectTimeout = defaultRemoteFileConnectTimeout
	}
	if result.tlsHandshakeTimeout <= 0 {
		result.tlsHandshakeTimeout = defaultRemoteFileTLSHandshakeTimeout
	}
	if result.responseHeaderTimeout <= 0 {
		result.responseHeaderTimeout = defaultRemoteFileResponseHeaderTimeout
	}
	if result.maxRedirects <= 0 {
		result.maxRedirects = defaultRemoteFileMaxRedirects
	}
	return &result
}

type secureRemoteFetchState struct {
	fetcher  *SecureRemoteFileFetcher
	mutex    sync.Mutex
	expected map[string][]netip.Addr
}

func (s *secureRemoteFetchState) validateAndPin(ctx context.Context, target *url.URL) error {
	authority, host, err := canonicalRemoteAuthority(target)
	if err != nil {
		return err
	}
	addresses, err := resolvePublicAddresses(ctx, s.fetcher.resolver, host)
	if err != nil {
		return err
	}
	s.mutex.Lock()
	s.expected[authority] = addresses
	s.mutex.Unlock()
	return nil
}

func (s *secureRemoteFetchState) dialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	connectContext, cancel := context.WithTimeout(ctx, s.fetcher.connectTimeout)
	defer cancel()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid destination authority: %w", err)
	}
	authority := net.JoinHostPort(strings.ToLower(host), port)
	s.mutex.Lock()
	expected := append([]netip.Addr(nil), s.expected[authority]...)
	s.mutex.Unlock()
	if len(expected) == 0 {
		return nil, errors.New("destination was not validated")
	}
	current, err := resolvePublicAddresses(connectContext, s.fetcher.resolver, host)
	if err != nil {
		return nil, err
	}
	if !sameAddressSet(expected, current) {
		return nil, errors.New("destination addresses changed between validation and connection")
	}
	pinned := net.JoinHostPort(current[0].String(), port)
	return s.fetcher.dialContext(connectContext, network, pinned)
}

func canonicalRemoteAuthority(target *url.URL) (string, string, error) {
	if target == nil || target.Opaque != "" {
		return "", "", errors.New("download URL must be hierarchical")
	}
	scheme := strings.ToLower(target.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", "", errors.New("download URL must use HTTP or HTTPS")
	}
	if target.User != nil {
		return "", "", errors.New("download URL must not contain embedded credentials")
	}
	host := strings.ToLower(target.Hostname())
	if host == "" {
		return "", "", errors.New("download URL host is required")
	}
	if strings.Contains(host, "%") {
		return "", "", errors.New("IPv6 zone identifiers are not allowed")
	}
	port := target.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", "", errors.New("download URL port is invalid")
	}
	return net.JoinHostPort(host, port), host, nil
}

func resolvePublicAddresses(ctx context.Context, resolver ipResolver, host string) ([]netip.Addr, error) {
	if parsed, err := netip.ParseAddr(host); err == nil {
		parsed = parsed.Unmap()
		if !isPublicRemoteAddress(parsed) {
			return nil, fmt.Errorf("destination address %s is not public", parsed)
		}
		return []netip.Addr{parsed}, nil
	}
	if err := validateRemoteHostname(host); err != nil {
		return nil, err
	}
	resolved, err := resolver.LookupIPAddr(ctx, host+".")
	if err != nil {
		return nil, fmt.Errorf("could not resolve destination host: %w", err)
	}
	if len(resolved) == 0 {
		return nil, errors.New("destination host resolved to no addresses")
	}
	unique := map[netip.Addr]struct{}{}
	for _, candidate := range resolved {
		if candidate.Zone != "" {
			return nil, errors.New("resolved address contains an IPv6 zone")
		}
		address, ok := netip.AddrFromSlice(candidate.IP)
		if !ok {
			return nil, errors.New("destination host resolved to an invalid address")
		}
		address = address.Unmap()
		if !isPublicRemoteAddress(address) {
			return nil, fmt.Errorf("destination address %s is not public", address)
		}
		unique[address] = struct{}{}
	}
	addresses := make([]netip.Addr, 0, len(unique))
	for address := range unique {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(i, j int) bool { return addresses[i].Less(addresses[j]) })
	return addresses, nil
}

func validateRemoteHostname(host string) error {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return errors.New("local destination hostnames are not allowed")
	}
	switch host {
	case "metadata.google.internal", "metadata.goog", "instance-data.ec2.internal":
		return errors.New("cloud metadata hostnames are not allowed")
	}
	if len(host) > 253 || strings.HasSuffix(host, ".") || !strings.Contains(host, ".") {
		return errors.New("destination hostname is ambiguous")
	}
	if looksLikeNonCanonicalIP(host) {
		return errors.New("non-canonical numeric hostnames are not allowed")
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("destination hostname is invalid")
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return errors.New("destination hostname contains unsupported characters")
			}
		}
	}
	return nil
}

func looksLikeNonCanonicalIP(host string) bool {
	allNumeric := true
	for _, character := range host {
		if (character < '0' || character > '9') && character != '.' {
			allNumeric = false
			break
		}
	}
	if allNumeric {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if strings.HasPrefix(label, "0x") {
			return true
		}
	}
	return false
}

var blockedRemotePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:10::/28"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func isPublicRemoteAddress(address netip.Addr) bool {
	if !address.IsValid() {
		return false
	}
	address = address.Unmap()
	if address.IsUnspecified() || address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() {
		return false
	}
	for _, prefix := range blockedRemotePrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func sameAddressSet(left []netip.Addr, right []netip.Addr) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
