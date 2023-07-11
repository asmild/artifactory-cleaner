package client

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	client *http.Client
	//artifactoryUsername string
	artifactoryUrl   *url.URL
	artifactoryToken string
}

// NewClient creates Client struct instance with http client configured to follow redirects
func NewClient(config Config) (*Client, error) {
	c := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 10 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			MaxIdleConnsPerHost: 128,
			IdleConnTimeout:     60 * time.Second,
		},
	}

	parsedUrl, err := url.Parse(config.Url)

	//util.Debug(false, "client.go  client init - Parsed Url", parsedUrl)

	if nil != err {
		return nil, err
		//return nil, error.New(err, fmt.Sprintf("error parsing api url: %s", config.Url))
	}
	//util.Debug(false, "client.go client init - config", config)

	return &Client{
		client: c,
		//artifactoryUsername: 	config.Username,
		artifactoryUrl:   parsedUrl,
		artifactoryToken: config.Token,
	}, nil
}

func (c *Client) ExecRaw(execPath string, method string, headers map[string]string, requestBody []byte) (*http.Response, error) {
	var err error

	rq, err := http.NewRequest(method, fmt.Sprintf("%s%s", c.artifactoryUrl.String(), execPath), bytes.NewReader(requestBody))
	if nil != err {
		return nil, fmt.Errorf("create request failed: %s", err)
	}

	for k, v := range headers {
		rq.Header.Add(k, v)
	}

	resp, err := c.client.Do(rq)

	if nil != err {
		return nil, fmt.Errorf("Do request failed: %s", err)
	}

	return resp, nil
}

func (c *Client) ExecDelete(execPath string) ([]byte, error) {
	h := make(map[string]string)
	var requestBody []byte
	token := c.artifactoryToken
	h["X-JFrog-Art-Api"] = token
	h["Content-Type"] = "text/plain"

	resp, err := c.ExecRaw(execPath, "DELETE", h, requestBody)
	if nil != err {
		return nil, err
	}

	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if nil != err {
		return nil, fmt.Errorf("response error: %s", err)
	}

	return respBody, err
}

// Exec method sends POST request to provided path and includes some default headers including api key
//func (c *Client) Exec(ctx context.Context, execPath string, payload ...interface{}) ([]byte, error) {
func (c *Client) Exec(execPath string, payload string) ([]byte, error) {
	var requestBody []byte
	var err error

	if len(payload) > 0 {
		requestBody = []byte(payload)
		//if nil != err {
		//	return nil, fmt.Errorf("error preparing http request body")
		//}
	}

	h := make(map[string]string)
	token := c.artifactoryToken
	h["X-JFrog-Art-Api"] = token
	h["Content-Type"] = "text/plain"
	//h["User-Agent"] = fmt.Sprintf("artifactory Go-cleaner %s/%s/%s", Version, Build, System)

	resp, err := c.ExecRaw(execPath, "POST", h, requestBody)
	if nil != err {
		return nil, err
	}

	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if nil != err {
		return nil, fmt.Errorf("response error: %s", err)
	}

	if resp.StatusCode >= 400 {
		//	if c.isJson(resp) {
		//		var eInfo ExceptionInfo
		//		err = json.Unmarshal(respBody, &eInfo)
		//		if nil != err {
		//			return nil, err
		//		}
		//		return nil, NewError(fmt.Errorf(eInfo.getTrace()), eInfo.getMessage())
		//	}
		return nil, fmt.Errorf("response code: %s", resp.Status)
	}
	if string(respBody) == "null" {
		return nil, nil
	}

	return respBody, nil
}
