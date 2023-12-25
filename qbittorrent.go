package qbittorrent

import (
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"time"

	"golang.org/x/net/publicsuffix"
)

var (
	DefaultTimeout = 60 * time.Second
)

type Client struct {
	cfg Config

	http    *http.Client
	timeout time.Duration

	log *log.Logger
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
		ForceAttemptHTTP2:     true,             // default is true; since HTTP/2 multiplexes a single TCP connection. we'd want to use HTTP/1, which would use multiple TCP connections.
		MaxIdleConns:          100,              // default transport value
		MaxIdleConnsPerHost:   10,               // default is 2, so we want to increase the number to use establish more connections.
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
