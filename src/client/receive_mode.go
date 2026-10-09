package client

import "os"

// Base refcount floor for the boot-time receive subscription: never
// released, so websocket subscribers attach/detach "above" it and the
// subscription survives for the process lifetime.
const baseSubscriptionRefCount = 1

// manualReceiveMode reports whether the signal-cli daemon needs explicit
// subscribeReceive calls (JSON_RPC_RECEIVE_MODE=manual; default on-start
// mode pushes notifications without subscriptions). Overridable via
// setManualReceiveMode for tests.
func (r *JsonRpc2Client) manualReceiveMode() bool {
	r.receiveModeMutex.Lock()
	defer r.receiveModeMutex.Unlock()

	if r.manualReceiveModeOverride != nil {
		return *r.manualReceiveModeOverride
	}
	return os.Getenv("JSON_RPC_RECEIVE_MODE") == "manual"
}

// setManualReceiveMode pins the manual-mode flag (test seam: the real
// value is derived from the JSON_RPC_RECEIVE_MODE env variable).
func (r *JsonRpc2Client) setManualReceiveMode(manual bool) {
	r.receiveModeMutex.Lock()
	defer r.receiveModeMutex.Unlock()

	r.manualReceiveModeOverride = &manual
}
