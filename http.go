package qbittorrent

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/autobrr/go-qbittorrent/errors"
	"github.com/avast/retry-go"
)

func (c *Client) get(endpoint string, opts map[string]string) (*http.Response, error) {
	return c.getCtx(
		context.Background(), endpoint, opts)
}

func (c *Client) getCtx(ctx context.Context, endpoint string, opts map[string]string) (*http.Response, error) {
	var err error
	var resp *http.Response

	reqUrl := c.buildUrl(endpoint, opts)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqUrl, nil)
	if err != nil {
		return nil, errors.Wrap(err, "could not build request")
	}

	if c.cfg.BasicUser != "" && c.cfg.BasicPass != "" {
		req.SetBasicAuth(c.cfg.BasicUser, c.cfg.BasicPass)
	}

	// try request and if fail run 10 retries
	err = retry.Do(func() error {
		resp, err = c.http.Do(req)
		if resp != nil && resp.StatusCode == http.StatusForbidden {
			if err := c.Login(); err != nil {
				return errors.Wrap(err, "qbit re-login failed")
			}
		} else if err != nil {
			return errors.Wrap(err, "qbit POST failed")
		}

		return err
	},
		retry.OnRetry(func(n uint, err error) { c.log.Printf("%q: attempt %d - %v\n", err, n, reqUrl) }),
		retry.Delay(time.Second*5),
		retry.Attempts(10),
		retry.MaxJitter(time.Second*1))

	if err != nil {
		return nil, errors.Wrap(err, "error making get request: %v", reqUrl)
	}

	return resp, nil
}

func (c *Client) post(endpoint string, opts map[string]string) (*http.Response, error) {
	return c.postCtx(context.Background(), endpoint, opts)
}

func (c *Client) postCtx(ctx context.Context, endpoint string, opts map[string]string) (*http.Response, error) {
	// add optional parameters that the user wants
	form := url.Values{}
	if opts != nil {
		for k, v := range opts {
			form.Add(k, v)
		}
	}

	var err error
	var resp *http.Response

	reqUrl := c.buildUrl(endpoint, nil)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqUrl, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, errors.Wrap(err, "could not build request")
	}

	if c.cfg.BasicUser != "" && c.cfg.BasicPass != "" {
		req.SetBasicAuth(c.cfg.BasicUser, c.cfg.BasicPass)
	}

	// add the content-type so qbittorrent knows what to expect
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	// try request and if fail run 10 retries
	err = retry.Do(func() error {
		resp, err = c.http.Do(req)
		if resp != nil && resp.StatusCode == http.StatusForbidden {
			if err := c.Login(); err != nil {
				return errors.Wrap(err, "qbit re-login failed")
			}
		} else if err != nil {
			return errors.Wrap(err, "qbit POST failed")
		}

		return err
	},
		retry.OnRetry(func(n uint, err error) { c.log.Printf("%q: attempt %d - %v\n", err, n, reqUrl) }),
		retry.Delay(time.Second*5),
		retry.Attempts(10),
		retry.MaxJitter(time.Second*1))

	if err != nil {
		return nil, errors.Wrap(err, "error making post request: %v", reqUrl)
	}

	return resp, nil
}

func (c *Client) postBasic(endpoint string, opts map[string]string) (*http.Response, error) {
	return c.postBasicCtx(context.Background(), endpoint, opts)
}

func (c *Client) postBasicCtx(ctx context.Context, endpoint string, opts map[string]string) (*http.Response, error) {
	// add optional parameters that the user wants
	form := url.Values{}
	if opts != nil {
		for k, v := range opts {
			form.Add(k, v)
		}
	}

	var err error
	var resp *http.Response

	reqUrl := c.buildUrl(endpoint, nil)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqUrl, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, errors.Wrap(err, "could not build request")
	}

	if c.cfg.BasicUser != "" && c.cfg.BasicPass != "" {
		req.SetBasicAuth(c.cfg.BasicUser, c.cfg.BasicPass)
	}

	// add the content-type so qbittorrent knows what to expect
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err = c.http.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "error making post request: %v", reqUrl)
	}

	return resp, nil
}

func (c *Client) postFile(endpoint string, fileName string, opts map[string]string) (*http.Response, error) {
	return c.postFileCtx(context.Background(), endpoint, fileName, opts)
}

func (c *Client) postFileCtx(ctx context.Context, endpoint string, fileName string, opts map[string]string) (*http.Response, error) {
	var err error
	var resp *http.Response

	file, err := os.Open(fileName)
	if err != nil {
		return nil, errors.Wrap(err, "error opening file %v", fileName)
	}
	// Close the file later
	defer file.Close()

	// Buffer to store our request body as bytes
	var requestBody bytes.Buffer

	// Store a multipart writer
	multiPartWriter := multipart.NewWriter(&requestBody)

	// Initialize file field
	fileWriter, err := multiPartWriter.CreateFormFile("torrents", fileName)
	if err != nil {
		return nil, errors.Wrap(err, "error initializing file field %v", fileName)
	}

	// Copy the actual file content to the fields writer
	_, err = io.Copy(fileWriter, file)
	if err != nil {
		return nil, errors.Wrap(err, "error copy file contents to writer %v", fileName)
	}

	// Populate other fields
	if opts != nil {
		for key, val := range opts {
			fieldWriter, err := multiPartWriter.CreateFormField(key)
			if err != nil {
				return nil, errors.Wrap(err, "error creating form field %v with value %v", key, val)
			}

			_, err = fieldWriter.Write([]byte(val))
			if err != nil {
				return nil, errors.Wrap(err, "error writing field %v with value %v", key, val)
			}
		}
	}

	// Close multipart writer
	multiPartWriter.Close()

	reqUrl := c.buildUrl(endpoint, nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqUrl, &requestBody)
	if err != nil {
		return nil, errors.Wrap(err, "error creating request %v", fileName)
	}

	if c.cfg.BasicUser != "" && c.cfg.BasicPass != "" {
		req.SetBasicAuth(c.cfg.BasicUser, c.cfg.BasicPass)
	}

	// Set correct content type
	req.Header.Set("Content-Type", multiPartWriter.FormDataContentType())

	// try request and if fail run 10 retries
	err = retry.Do(func() error {
		resp, err = c.http.Do(req)
		if resp != nil && resp.StatusCode == http.StatusForbidden {
			if err := c.Login(); err != nil {
				return errors.Wrap(err, "qbit re-login failed")
			}
		} else if err != nil {
			return errors.Wrap(err, "qbit POST failed")
		}

		return err
	},
		retry.OnRetry(func(n uint, err error) { c.log.Printf("%q: attempt %d - %v\n", err, n, reqUrl) }),
		retry.Delay(time.Second*5),
		retry.Attempts(10),
		retry.MaxJitter(time.Second*1))

	if err != nil {
		return nil, errors.Wrap(err, "error making post file request %v", fileName)
	}

	return resp, nil
}

func (c *Client) setCookies(cookies []*http.Cookie) {
	cookieURL, _ := url.Parse(c.buildUrl(c.cfg.Host, nil))

	c.http.Jar.SetCookies(cookieURL, cookies)
}

func (c *Client) buildUrl(endpoint string, params map[string]string) string {
	apiBase := "/api/v2/"

	// add query params
	queryParams := url.Values{}
	if params != nil {
		for key, value := range params {
			queryParams.Add(key, value)
		}
	}

	encodedValues := queryParams.Encode()

	joinedUrl, _ := url.JoinPath(c.cfg.Host, apiBase, endpoint, encodedValues)

	escapedUrl, _ := url.QueryUnescape(joinedUrl)

	// make into new string and return
	return escapedUrl
}
