package network

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/coreos/go-iptables/iptables"
	"github.com/vishvananda/netlink"
)

const (
	Subnet = "172.16.0.0/16"
	// Linux caps interface names at 15 characters, so a full UUID won't fit.
	TapPrefix = "sp-"
)

// Network routes VM traffic from the taps to the internet through the host's
// default interface.
type Network struct {
	egress     string
	ipt        *iptables.IPTables
	masquerade []string
	forward    [][]string

	pool
}

func Setup() (*Network, error) {
	route, err := netlink.RouteGet(net.IPv4(1, 1, 1, 1))
	if err != nil {
		return nil, fmt.Errorf("finding the default route: %w", err)
	}
	link, err := netlink.LinkByIndex(route[0].LinkIndex)
	if err != nil {
		return nil, err
	}
	egress := link.Attrs().Name

	links, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		if strings.HasPrefix(l.Attrs().Name, TapPrefix) {
			if err := netlink.LinkDel(l); err != nil {
				return nil, fmt.Errorf("deleting stale tap %s: %w", l.Attrs().Name, err)
			}
		}
	}

	if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0o644); err != nil {
		return nil, fmt.Errorf("enabling ip_forward: %w", err)
	}

	ipt, err := iptables.New()
	if err != nil {
		return nil, err
	}
	n := &Network{
		egress:     egress,
		ipt:        ipt,
		masquerade: []string{"-s", Subnet, "-o", egress, "-j", "MASQUERADE"},
		// Docker sets the FORWARD policy to DROP, so these go at the top of
		// the chain. The last rule stops one VM from reaching another.
		forward: [][]string{
			{"-i", TapPrefix + "+", "-o", egress, "-j", "ACCEPT"},
			{"-i", egress, "-o", TapPrefix + "+", "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
			{"-i", TapPrefix + "+", "-j", "DROP"},
		},
	}
	if err := ipt.AppendUnique("nat", "POSTROUTING", n.masquerade...); err != nil {
		return nil, err
	}
	for i, rule := range n.forward {
		if err := ipt.InsertUnique("filter", "FORWARD", i+1, rule...); err != nil {
			return nil, err
		}
	}
	return n, nil
}

func (n *Network) Egress() string {
	return n.egress
}

func (n *Network) Close() error {
	if err := n.ipt.DeleteIfExists("nat", "POSTROUTING", n.masquerade...); err != nil {
		return err
	}
	for _, rule := range n.forward {
		if err := n.ipt.DeleteIfExists("filter", "FORWARD", rule...); err != nil {
			return err
		}
	}
	return nil
}

func CreateTap(name string, lease Lease) error {
	la := netlink.NewLinkAttrs()
	la.Name = name
	tap := &netlink.Tuntap{LinkAttrs: la, Mode: netlink.TUNTAP_MODE_TAP}
	if err := netlink.LinkAdd(tap); err != nil {
		return fmt.Errorf("creating tap %s: %w", name, err)
	}

	// udev replaces a new interface's random MAC a few milliseconds after it
	// appears, and a MAC change flushes the ARP entry added below. udev leaves
	// a MAC alone once something has set it.
	if err := netlink.LinkSetHardwareAddr(tap, lease.HostMAC()); err != nil {
		_ = netlink.LinkDel(tap)
		return fmt.Errorf("setting the mac of tap %s: %w", name, err)
	}

	addr := &netlink.Addr{IPNet: &net.IPNet{IP: lease.Host, Mask: Mask}}
	if err := netlink.AddrAdd(tap, addr); err != nil {
		_ = netlink.LinkDel(tap)
		return fmt.Errorf("adding %s to tap %s: %w", lease.Host, name, err)
	}
	if err := netlink.LinkSetUp(tap); err != nil {
		_ = netlink.LinkDel(tap)
		return fmt.Errorf("bringing up tap %s: %w", name, err)
	}

	// Without this, a connection made before the guest is up fails its ARP
	// lookup, and Linux waits a second before it asks again.
	neigh := &netlink.Neigh{
		LinkIndex:    tap.Attrs().Index,
		Family:       netlink.FAMILY_V4,
		State:        netlink.NUD_PERMANENT,
		IP:           lease.Guest,
		HardwareAddr: lease.GuestMAC(),
	}
	if err := netlink.NeighSet(neigh); err != nil {
		_ = netlink.LinkDel(tap)
		return fmt.Errorf("adding arp entry for %s: %w", lease.Guest, err)
	}
	return nil
}

func DeleteTap(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return err
	}
	return netlink.LinkDel(link)
}
