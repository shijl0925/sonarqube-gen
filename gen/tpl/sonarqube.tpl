package sonarqube

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-playground/form/v4"
	"github.com/google/go-querystring/query"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
)

const (
	basicAuth int = iota
	privateToken
	Anonymous
)

type Client struct {
	client   *http.Client
	host      string
	username string
	password string
	token    string
	authType int

{{- range .Services}}
	{{.Getter}} *{{.Getter}}
{{- end }}
}

type service struct {
	client *Client
	path   string
}

func NewClient(sonarURL string, username string, password string, client *http.Client) *Client {
	if client == nil {
		client = &http.Client{}
	}

	var authType int
	if len(username) != 0 && len(password) != 0 {
		authType = basicAuth
	} else {
		authType = Anonymous
	}

	c := &Client{
		client:   client,
		username: username,
		password: password,
		authType: authType,
	}

	c.host = sonarURL

{{- range .Services}}
	c.{{.Getter}} = &{{.Getter}}{client: c, path: "{{.Path}}"}
{{- end }}

	return c
}

func NewClientByToken(sonarURL string, token string, client *http.Client) *Client {
	c := NewClient(sonarURL, "", "", client)
	c.token = token

	var authType int
	if len(token) != 0 {
		authType = privateToken
	} else {
		authType = Anonymous
	}
	c.authType = authType
	return c
}

// 封装认证处理
func (c *Client) handleAuth(req *http.Request) {
	switch c.authType {
	case basicAuth:
		req.SetBasicAuth(c.username, c.password)
	case privateToken:
		req.SetBasicAuth(c.token, "")
	default:
		// do nothing
	}
}

func (c *Client) NewRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	// 认证处理
	c.handleAuth(req)

	// 设置通用请求头
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	return req, nil
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.client.Do(req)

	if err != nil {
		return nil, fmt.Errorf("error trying to execute request: %+v", err)
	}

	if resp.StatusCode >= 300 {
		if errorResponse, err := ErrorResponseFrom(resp); err != nil {
			return nil, fmt.Errorf("received non 2xx status code (%d), but could not decode error response: %+v", resp.StatusCode, err)
		} else {
			return nil, errorResponse
		}
	}
	return resp, nil
}

func (c *Client) Call(ctx context.Context, method string, u string, v interface{}, opt ...interface{}) (*http.Response, error) {
	u = fmt.Sprintf("%s/%s", c.host, u)
	var req *http.Request
	var err error

	if method == http.MethodGet {
		for _, o := range opt {
			urlStr, err := addOptions(u, o)
			if err != nil {
				return nil, err
			}
			u = urlStr
		}

		req, err = c.NewRequest(ctx, "GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("could not create request: %v", err)
		}
	} else {
		encoder := form.NewEncoder()
		values, err := encoder.Encode(opt[0])
		if err != nil {
			return nil, fmt.Errorf("could not encode form values: %v", err)
		}
		req, err = c.NewRequest(ctx, "POST", u, strings.NewReader(values.Encode()))
		if err != nil {
			return nil, fmt.Errorf("could not create request: %v", err)
		}
	}

	isText := false

	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error trying to execute request: %+v", err)
	}

	if v != nil {
		defer resp.Body.Close()

		res := reflect.ValueOf(v).Elem()
		if res.Kind() == reflect.String {
			isText = true
		}

		if isText {
			buf := new(strings.Builder)
			_, err := io.Copy(buf, resp.Body)
			if err != nil {
				return resp, fmt.Errorf("could not read response body: %v", err)
			}
			res.SetString(buf.String())
		} else {
			if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
				return nil, fmt.Errorf("could not decode response: %v", err)
			}
		}
	}
	return resp, err
}

func addOptions(s string, opt interface{}) (string, error) {
	v := reflect.ValueOf(opt)

	if v.Kind() == reflect.Ptr && v.IsNil() {
		return s, nil
	}

	origURL, err := url.Parse(s)
	if err != nil {
		return s, err
	}

	origValues := origURL.Query()

	newValues, err := query.Values(opt)
	if err != nil {
		return s, err
	}

	for k, v := range newValues {
		origValues[k] = v
	}

	origURL.RawQuery = origValues.Encode()

	return origURL.String(), nil
}
