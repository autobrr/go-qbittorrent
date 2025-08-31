package qbittorrent

import (
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"time"

	"github.com/Masterminds/semver"
	"golang.org/x/net/publicsuffix"
)

var (
	DefaultTimeout = 60 * time.Second
)

type Client struct {
	cfg Config

	http    *http.Client
	timeout time.Duration

	retryAttempts int
	retryDelay    time.Duration

	log *log.Logger

	version *semver.Version
}

type Config struct {
	Host     string
	Username string
	Password string

	// TLS skip cert validation
	TLSSkipVerify bool

	// HTTP Basic auth username
	BasicUser string

	// HTTP Basic auth password
	BasicPass string

	Timeout int
	Log     *log.Logger

	// Retry settings
	RetryAttempts int
	RetryDelay    int // in seconds
}

func NewClient(cfg Config) *Client {
	c := &Client{
		cfg:     cfg,
		log:     log.New(io.Discard, "", log.LstdFlags),
		timeout: DefaultTimeout,
	}

	// override logger if we pass one
	if cfg.Log != nil {
		c.log = cfg.Log
	}

	if cfg.Timeout > 0 {
		c.timeout = time.Duration(cfg.Timeout) * time.Second
	}

	// set retry defaults
	c.retryAttempts = 5
	c.retryDelay = 1

	if cfg.RetryAttempts > 0 {
		c.retryAttempts = cfg.RetryAttempts
	}

	if cfg.RetryDelay > 0 {
		c.retryDelay = time.Duration(cfg.RetryDelay) * time.Second
	}

	//store cookies in jar
	jarOptions := &cookiejar.Options{PublicSuffixList: publicsuffix.List}
	jar, err := cookiejar.New(jarOptions)
	if err != nil {
		c.log.Println("new client cookie error")
	}

	customTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second, // default transport value
			KeepAlive: 30 * time.Second, // default transport value
		}).DialContext,
		ForceAttemptHTTP2:     true,             // HTTP/2 provides better multiplexing for API calls to the same host
		MaxIdleConns:          100,              // default transport value
		MaxIdleConnsPerHost:   10,               // increased from default 2 for better connection reuse
		IdleConnTimeout:       90 * time.Second, // default transport value
		TLSHandshakeTimeout:   10 * time.Second, // default transport value
		ExpectContinueTimeout: 1 * time.Second,  // default transport value
		ReadBufferSize:        65536,
		WriteBufferSize:       65536,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.TLSSkipVerify,
		},
	}

	c.http = &http.Client{
		Jar:       jar,
		Timeout:   c.timeout,
		Transport: customTransport,
	}

	return c
}

// WithHTTPClient allows you to a provide a custom [http.Client].
func (c *Client) WithHTTPClient(client *http.Client) *Client {
	client.Jar = c.http.Jar
	c.http = client
	return c
}

// GetHTTPClient allows you to a receive the implemented [http.Client].
func (c *Client) GetHTTPClient() *http.Client {
	return c.http
}
