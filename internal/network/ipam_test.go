package network

import (
	"errors"
	"net"
	"testing"
)

func TestPool(t *testing.T) {
	var p pool

	first, _ := p.Allocate()
	second, _ := p.Allocate()
	if !first.Host.Equal(net.IPv4(172, 16, 0, 1)) || !first.Guest.Equal(net.IPv4(172, 16, 0, 2)) {
		t.Fatalf("first lease = %s/%s", first.Host, first.Guest)
	}
	if !second.Host.Equal(net.IPv4(172, 16, 0, 5)) || !second.Guest.Equal(net.IPv4(172, 16, 0, 6)) {
		t.Fatalf("second lease = %s/%s", second.Host, second.Guest)
	}

	p.Release(first)
	again, _ := p.Allocate()
	if !again.Guest.Equal(first.Guest) {
		t.Fatalf("released lease not reused: got %s", again.Guest)
	}

	var last Lease
	for {
		l, err := p.Allocate()
		if errors.Is(err, ErrPoolExhausted) {
			break
		}
		last = l
	}
	if !last.Guest.Equal(net.IPv4(172, 16, 255, 254)) {
		t.Fatalf("last lease = %s", last.Guest)
	}
}
