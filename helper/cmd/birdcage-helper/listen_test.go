package main

import "testing"

// TestListenLoopbackOnly: Helper는 루프백에만 바인딩한다(ADR 0002 전제 P-0002-b).
func TestListenLoopbackOnly(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", "192.168.0.10:0", ":0", "[::]:0"} {
		if ln, err := listenLoopback(addr); err == nil {
			ln.Close()
			t.Errorf("%s: non-loopback listen must be refused", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:0", "localhost:0"} {
		ln, err := listenLoopback(addr)
		if err != nil {
			t.Errorf("%s: %v", addr, err)
			continue
		}
		ln.Close()
	}
}
