package herdr

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"time"
)

// attempts are per-try deadlines: a busy server gets one longer second chance.
var attempts = []time.Duration{500 * time.Millisecond, 1500 * time.Millisecond}

var reqCounter atomic.Uint64

// unixSender speaks Herdr's newline-delimited JSON protocol on its unix socket.
type unixSender struct{ path string }

func (u unixSender) send(method string, params map[string]any) error {
	var err error
	for _, d := range attempts {
		if err = u.try(method, params, d); err == nil {
			return nil
		}
		// a server that answered with an error will not change its mind
		if _, ok := err.(serverError); ok {
			return err
		}
	}
	return err
}

func (u unixSender) try(method string, params map[string]any, d time.Duration) error {
	c, err := net.DialTimeout("unix", u.path, d)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(d))
	req := map[string]any{
		"id":     fmt.Sprintf("%s:%d:%d", source, time.Now().UnixMilli(), reqCounter.Add(1)),
		"method": method,
		"params": params,
	}
	if err := json.NewEncoder(c).Encode(req); err != nil {
		return err
	}
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		return err
	}
	var resp struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return err
	}
	if resp.Error != nil {
		return serverError(resp.Error.Code + ": " + resp.Error.Message)
	}
	return nil
}

type serverError string

func (e serverError) Error() string { return string(e) }
