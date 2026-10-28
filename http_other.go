//go:build !windows

package main

import (
	"io"
	"net/http"
)

func httpGet(target, accept string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", accept)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	return body, res.StatusCode, err
}
