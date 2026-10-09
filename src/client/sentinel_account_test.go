package client

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"

	"github.com/bbernhard/signal-cli-rest-api/utils"
	"github.com/stretchr/testify/assert"
)

// Test_SubscribeReceive_SentinelOmitsAccountParam verifies the
// MULTI_ACCOUNT_NUMBER sentinel is NOT serialized as params.account —
// the daemon dispatches the multi-command variant (subscribe all
// managers + future ones) only without the account param; with the
// sentinel it fails NotRegistered (cluster incident 2026-10-09).
func Test_SubscribeReceive_SentinelOmitsAccountParam(t *testing.T) {
	// Given: a fake json-rpc daemon that records the raw request line
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	defer func() { _ = ln.Close() }()

	reqCh := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		buf := make([]byte, 4096)
		n, _ := conn.Read(buf)
		reqCh <- buf[:n]
		// reply with the same request id so getRaw's routing matches
		var req struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(buf[:n], &req) != nil || req.ID == "" {
			_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":"","result":7}` + "\n"))
			return
		}
		resp := fmt.Sprintf(`{"jsonrpc":"2.0","id":%q,"result":7}`+"\n", req.ID)
		_, _ = conn.Write([]byte(resp))
	}()

	c := NewJsonRpc2Client(utils.NewSignalCliApiConfig(), utils.MULTI_ACCOUNT_NUMBER)
	assert.NoError(t, c.Dial(ln.Addr().String(), 1))
	go c.ReceiveData(utils.MULTI_ACCOUNT_NUMBER, "")

	// When: subscribing with the sentinel
	id, err := c.subscribeReceive(utils.MULTI_ACCOUNT_NUMBER)

	// Then: call succeeds and the wire request carries no account param
	assert.NoError(t, err)
	assert.Equal(t, int64(7), id)
	raw := <-reqCh
	var req map[string]any
	assert.NoError(t, json.Unmarshal(raw, &req))
	params, ok := req["params"].(map[string]any)
	if ok {
		_, has := params["account"]
		assert.False(t, has, "sentinel must not leak as params.account: %s", raw)
	}
}
