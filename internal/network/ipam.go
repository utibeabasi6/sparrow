package network

import (
	"errors"
	"net"
	"sync"
)

var ErrPoolExhausted = errors.New("no free addresses")

// Mask is a /30: four addresses, of which two are usable.
var Mask = net.CIDRMask(30, 32)

// Lease is one /30 from Subnet. The tap gets Host and the VM gets Guest.
type Lease struct {
	Host  net.IP
	Guest net.IP
	slot  int
}

// GuestMAC and HostMAC embed their IP, so no two interfaces share a MAC.
func (l Lease) GuestMAC() net.HardwareAddr { return mac(l.Guest) }
func (l Lease) HostMAC() net.HardwareAddr  { return mac(l.Host) }

func mac(ip net.IP) net.HardwareAddr {
	v4 := ip.To4()
	return net.HardwareAddr{0x02, 0xfc, v4[0], v4[1], v4[2], v4[3]}
}

// pool splits Subnet into 16384 /30s.
type pool struct {
	mu   sync.Mutex
	used [1 << 14]bool
}

func (p *pool) Allocate() (Lease, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for slot, used := range p.used {
		if used {
			continue
		}
		p.used[slot] = true
		return Lease{Host: addr(slot, 1), Guest: addr(slot, 2), slot: slot}, nil
	}
	return Lease{}, ErrPoolExhausted
}

func (p *pool) Release(lease Lease) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.used[lease.slot] = false
}

func addr(slot, host int) net.IP {
	return net.IPv4(172, 16, byte(slot/64), byte(slot%64*4+host))
}
