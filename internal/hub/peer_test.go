package hub

// httptest gives every request the peer 192.0.2.1, a documentation address
// outside the private set. The handler tests are about other things, so
// that one peer passes; the network guard's tests use other addresses.
func init() { trustTestPeer = true }
