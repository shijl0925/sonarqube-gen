// Copyright 2013 The go-github AUTHORS. All rights reserved.
// Copyright 2021 Reinoud Kruithof
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sonarqube

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-playground/form/v4"
	"io"
	"io/ioutil"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/iancoleman/strcase"
)

var API string

const (
	basicAuth int = iota
	privateToken
	Anonymous int = iota
)

type Client struct {
	client   *http.Client
	API      string
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

	API = sonarURL

{{- range .Services}}
	c.{{.Getter}} = &{{.Getter}}{client: c}
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

func (c *Client) PostRequest(ctx context.Context, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return nil, err
	}

	switch c.authType {
	case basicAuth:
		req.SetBasicAuth(c.username, c.password)
	case privateToken:
		req.SetBasicAuth(c.token, "")
	default:
		// do nothing
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	return req, nil
}

func (c *Client) GetRequest(ctx context.Context, url string, params ...string) (*http.Request, error) {
	if l := len(params); l%2 != 0 {
		return nil, fmt.Errorf("params must be an even number, %d given", l)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()

	for i := 0; i < len(params); i++ {
		q.Add(params[i], params[i+1])
		i++
	}
	req.URL.RawQuery = q.Encode()

	switch c.authType {
	case basicAuth:
		req.SetBasicAuth(c.username, c.password)
	case privateToken:
		req.SetBasicAuth(c.token, "")
	default:
		// do nothing
	}

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
	var req *http.Request
	var err error
	if method == http.MethodGet {
		params := paramsFrom(opt...)
		req, err = c.GetRequest(ctx, u, params...)
		if err != nil {
			return nil, fmt.Errorf("could not create request: %+v", err)
		}
	} else {
		encoder := form.NewEncoder()
		values, err := encoder.Encode(opt)
		if err != nil {
			return nil, fmt.Errorf("could not encode form values: %+v", err)
		}
		req, err = c.PostRequest(ctx, u, strings.NewReader(values.Encode()))
		if err != nil {
			return nil, fmt.Errorf("could not create request: %+v", err)
		}
	}

	isText := false
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("v must be a pointer type")
	}
	res := val.Elem()
	if res.Kind() == reflect.String {
		req.Header.Set("Accept", "text/plain")
		isText = true
	}

	resp, err := c.Do(req)

	if v != nil {
		defer resp.Body.Close()

		if isText {
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				return resp, err
			}
			w := val.Elem()
			w.SetString(string(body))
		} else {
			err = json.NewDecoder(resp.Body).Decode(v)
			if err != nil {
				return nil, fmt.Errorf("could not decode response: %+v", err)
			}
		}
	}
	return resp, err
}

// paramsFrom creates a slice with interleaving param and value entries, i.e. ["key1", "value1", "key2, "value2"]
func paramsFrom(items ...interface{}) []string {
	allParams := make([]string, 0)

	for _, item := range items {
		v := reflect.ValueOf(item)
		t := v.Type()

		params := make([]string, 2*v.NumField())

		for i := 0; i < v.NumField(); i++ {
			j := i * 2
			k := j + 1

			if v.Field(i).IsZero() {
				continue
			}

			// Convert some basic types to strings for convenience.
			// Note: other types should not be used as parameter values.
			fieldValue := ""
			switch t.Field(i).Type.Name() {
			case "int":
				fieldValue = strconv.Itoa(v.Field(i).Interface().(int))
			case "string":
				fieldValue = v.Field(i).Interface().(string)
			case "bool":
				fieldValue = strconv.FormatBool(v.Field(i).Interface().(bool))
			}

			params[j] = strcase.ToLowerCamel(t.Field(i).Name)
			params[k] = fieldValue
		}

		allParams = append(allParams, params...)
	}

	return allParams
}
