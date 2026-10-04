// Package api is the HTTP client for the Zero Series API.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/KalebHawkins/zero/spec"
)

// Timeout is the limit for one request.
const Timeout = 15 * time.Second

// maxBody limits how much of an answer the client reads.
const maxBody = 32 << 20

// Client talks to one site. BaseURL is the site root; it may carry a path
// prefix, for example https://example.org/zero-next.
type Client struct {
	BaseURL   string
	Token     string
	UserAgent string
	HTTP      *http.Client
}

// New returns a client for the site root baseURL.
func New(baseURL, token, version string) *Client {
	return &Client{
		BaseURL:   baseURL,
		Token:     token,
		UserAgent: "zero/" + version,
		HTTP:      &http.Client{Timeout: Timeout},
	}
}

// Error is a 4xx or 5xx answer. Message is the server's sentence.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// NetError means the request never got an answer.
type NetError struct {
	BaseURL string
	Err     error
}

func (e *NetError) Error() string {
	return fmt.Sprintf("Cannot reach %s: %v", e.BaseURL, e.Err)
}

func (e *NetError) Unwrap() error { return e.Err }

// Errors of the token poll.
var (
	ErrPending = errors.New("the code is not approved yet")
	ErrExpired = errors.New("the code expired")
)

// URL joins the site root, /api and an API path such as "/me".
func (c *Client) URL(path string) string {
	return strings.TrimRight(c.BaseURL, "/") + "/api/" + strings.TrimLeft(path, "/")
}

// do sends one request. in is sent as JSON when it is not nil. The answer is
// decoded into out when it is not nil and the answer has a body. do returns
// the status code of a 2xx answer.
func (c *Client) do(method, path string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.URL(path), body)
	if err != nil {
		return 0, &NetError{BaseURL: c.BaseURL, Err: err}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: Timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // drop the method and URL; the message names the site already
		}
		return 0, &NetError{BaseURL: c.BaseURL, Err: err}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return 0, &NetError{BaseURL: c.BaseURL, Err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return 0, decodeError(resp.StatusCode, data)
	}
	if out != nil && resp.StatusCode != http.StatusNoContent && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return 0, &Error{Status: resp.StatusCode, Code: "bad_answer",
				Message: fmt.Sprintf("%s sent an answer that is not the expected JSON. Check that api_url is the site address.", c.BaseURL)}
		}
	}
	return resp.StatusCode, nil
}

func decodeError(status int, data []byte) *Error {
	e := &Error{Status: status}
	var body spec.Error
	if json.Unmarshal(data, &body) == nil {
		e.Code, e.Message = body.Code, strings.TrimSpace(body.Message)
	}
	if e.Message == "" {
		e.Message = fmt.Sprintf("The server answered %d %s.", status, http.StatusText(status))
	}
	return e
}

// ServerConfig calls GET /api/config. It needs no sign-in.
func (c *Client) ServerConfig() (*spec.ServerConfig, error) {
	var out spec.ServerConfig
	_, err := c.do(http.MethodGet, "/config", nil, &out)
	return &out, err
}

// Me calls GET /api/me.
func (c *Client) Me() (*spec.User, error) {
	var out spec.MeResponse
	if _, err := c.do(http.MethodGet, "/me", nil, &out); err != nil {
		return nil, err
	}
	return &out.User, nil
}

// Device calls POST /api/cli/device to begin a login.
func (c *Client) Device(in spec.DeviceRequest) (*spec.DeviceResponse, error) {
	var out spec.DeviceResponse
	if _, err := c.do(http.MethodPost, "/cli/device", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PollToken calls POST /api/cli/token once. It returns ErrPending for 202 and
// ErrExpired for 410.
func (c *Client) PollToken(deviceCode string) (*spec.TokenResponse, error) {
	var out spec.TokenResponse
	status, err := c.do(http.MethodPost, "/cli/token", spec.TokenRequest{DeviceCode: deviceCode}, &out)
	var apiErr *Error
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusGone {
		return nil, ErrExpired
	}
	if err != nil {
		return nil, err
	}
	if status == http.StatusAccepted {
		return nil, ErrPending
	}
	return &out, nil
}

// Logout calls POST /api/cli/logout, which ends the client's token.
func (c *Client) Logout() error {
	_, err := c.do(http.MethodPost, "/cli/logout", nil, nil)
	return err
}

// PostDoctor calls POST /api/doctor.
func (c *Client) PostDoctor(d spec.Doctor) error {
	_, err := c.do(http.MethodPost, "/doctor", d, nil)
	return err
}

// Exercise calls GET /api/exercises/{id}. The server marks the exercise
// started.
func (c *Client) Exercise(id string) (*spec.ExerciseResponse, error) {
	return c.exercise(id, false)
}

// ExerciseWithReference calls GET /api/exercises/{id}?reference=1. For a
// stage, the answer also holds the previous stage's reference solution.
func (c *Client) ExerciseWithReference(id string) (*spec.ExerciseResponse, error) {
	return c.exercise(id, true)
}

func (c *Client) exercise(id string, reference bool) (*spec.ExerciseResponse, error) {
	var out spec.ExerciseResponse
	path := "/exercises/" + url.PathEscape(id)
	if reference {
		path += "?reference=1"
	}
	if _, err := c.do(http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PostRun calls POST /api/exercises/{id}/runs.
func (c *Client) PostRun(id string, r spec.Report) error {
	_, err := c.do(http.MethodPost, "/exercises/"+url.PathEscape(id)+"/runs", r, nil)
	return err
}

// Submit calls POST /api/exercises/{id}/submit.
func (c *Client) Submit(id string, in spec.SubmitRequest) (*spec.SubmitResponse, error) {
	var out spec.SubmitResponse
	if _, err := c.do(http.MethodPost, "/exercises/"+url.PathEscape(id)+"/submit", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Next calls GET /api/next.
func (c *Client) Next() (*spec.Next, error) {
	var out spec.NextResponse
	if _, err := c.do(http.MethodGet, "/next", nil, &out); err != nil {
		return nil, err
	}
	return out.Next, nil
}
