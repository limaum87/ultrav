package libvirt

import (
	"context"
	"encoding/xml"
	"fmt"
	"net"
	"regexp"
	"sort"

	libvirt "libvirt.org/go/libvirt"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

// poolXML is the subset of storage pool XML we consume.
type poolXML struct {
	Type   string `xml:"type,attr"`
	Target struct {
		Path string `xml:"path"`
	} `xml:"target"`
}

// networkXML is the subset of network XML we consume.
type networkXML struct {
	Bridge struct {
		Name string `xml:"name,attr"`
	} `xml:"bridge"`
	Forward struct {
		Mode string `xml:"mode,attr"`
	} `xml:"forward"`
	IP struct {
		Address string    `xml:"address,attr"`
		Prefix  int       `xml:"prefix,attr"`
		DHCP    *struct{} `xml:"dhcp"`
		Domain  struct {
			Name string `xml:"name,attr"`
		} `xml:"domain"`
	} `xml:"ip"`
}

// ListStoragePools returns every storage pool (active + inactive).
func (p *Provider) ListStoragePools(_ context.Context) ([]types.StoragePool, error) {
	var out []types.StoragePool
	err := p.withConn(func(c *libvirt.Connect) error {
		pools, err := c.ListAllStoragePools(
			libvirt.CONNECT_LIST_STORAGE_POOLS_ACTIVE | libvirt.CONNECT_LIST_STORAGE_POOLS_INACTIVE)
		if err != nil {
			return err
		}
		out = make([]types.StoragePool, 0, len(pools))
		for _, pool := range pools {
			m, err := poolToModel(&pool)
			if err != nil {
				continue
			}
			out = append(out, m)
		}
		sortPools(out)
		return nil
	})
	return out, err
}

func (p *Provider) GetStoragePool(_ context.Context, id string) (types.StoragePool, error) {
	var m types.StoragePool
	err := p.withConn(func(c *libvirt.Connect) error {
		pool, err := c.LookupStoragePoolByName(id)
		if err != nil {
			return hypervisor.ErrPoolNotFound
		}
		m, err = poolToModel(pool)
		return err
	})
	return m, err
}

// CreateStoragePool defines, builds and starts a directory-backed pool.
func (p *Provider) CreateStoragePool(_ context.Context, req types.StoragePoolCreate) (types.StoragePool, error) {
	poolType := "dir"
	if req.Type != nil && *req.Type != "" {
		poolType = string(*req.Type)
	}
	xmlDef := fmt.Sprintf(`<pool type=%q>
  <name>%s</name>
  <target>
    <path>%s</path>
  </target>
</pool>`, poolType, xmlEscape(req.Name), xmlEscape(req.TargetPath))

	err := p.withConn(func(c *libvirt.Connect) error {
		if _, err := c.LookupStoragePoolByName(req.Name); err == nil {
			return hypervisor.ErrPoolAlreadyExists
		}
		pool, err := c.StoragePoolDefineXML(xmlDef, 0)
		if err != nil {
			return err
		}
		if err := pool.Build(libvirt.STORAGE_POOL_BUILD_NEW); err != nil {
			_ = pool.Undefine()
			return err
		}
		if err := pool.Create(0); err != nil {
			_ = pool.Undefine()
			return err
		}
		autostart := req.Autostart == nil || *req.Autostart
		_ = pool.SetAutostart(autostart) // non-fatal on failure
		return nil
	})
	if err != nil {
		return types.StoragePool{}, err
	}
	return p.GetStoragePool(context.Background(), req.Name)
}

func (p *Provider) RefreshStoragePool(_ context.Context, id string) (types.StoragePool, error) {
	err := p.withConn(func(c *libvirt.Connect) error {
		pool, err := c.LookupStoragePoolByName(id)
		if err != nil {
			return hypervisor.ErrPoolNotFound
		}
		active, err := pool.IsActive()
		if err != nil {
			return err
		}
		if !active {
			return fmt.Errorf("%w: cannot refresh inactive storage pool", hypervisor.ErrInvalidNetworkState)
		}
		return pool.Refresh(0)
	})
	if err != nil {
		return types.StoragePool{}, err
	}
	return p.GetStoragePool(context.Background(), id)
}

func poolToModel(pool *libvirt.StoragePool) (types.StoragePool, error) {
	name, err := pool.GetName()
	if err != nil {
		return types.StoragePool{}, err
	}
	info, err := pool.GetInfo()
	if err != nil {
		return types.StoragePool{}, err
	}
	active, err := pool.IsActive()
	if err != nil {
		return types.StoragePool{}, err
	}
	autostart, err := pool.GetAutostart()
	if err != nil {
		autostart = false
	}

	state := types.StoragePoolStateInactive
	if active {
		state = types.StoragePoolStateActive
	}

	var px poolXML
	if xmlStr, err := pool.GetXMLDesc(0); err == nil {
		_ = xml.Unmarshal([]byte(xmlStr), &px)
	}

	autostartBool := autostart
	target := px.Target.Path
	return types.StoragePool{
		Id:              name,
		Name:            name,
		Type:            poolType(px.Type),
		State:           state,
		Autostart:       &autostartBool,
		CapacityBytes:   int64(info.Capacity),
		AllocationBytes: int64(info.Allocation),
		AvailableBytes:  int64(info.Available),
		TargetPath:      &target,
	}, nil
}

func poolType(t string) types.StoragePoolType {
	switch t {
	case "dir", "fs", "netfs", "lvm", "disk", "iscsi", "zfs", "rbd":
		return types.StoragePoolType(t)
	}
	return types.StoragePoolTypeOther
}

func sortPools(pools []types.StoragePool) {
	for i := 1; i < len(pools); i++ {
		for j := i; j > 0 && pools[j].Id < pools[j-1].Id; j-- {
			pools[j], pools[j-1] = pools[j-1], pools[j]
		}
	}
}

// --- networks ---

func (p *Provider) ListNetworks(_ context.Context) ([]types.Network, error) {
	var out []types.Network
	err := p.withConn(func(c *libvirt.Connect) error {
		nets, err := c.ListAllNetworks(
			libvirt.CONNECT_LIST_NETWORKS_ACTIVE | libvirt.CONNECT_LIST_NETWORKS_INACTIVE)
		if err != nil {
			return err
		}
		out = make([]types.Network, 0, len(nets))
		for _, n := range nets {
			m, err := networkToModel(&n)
			if err != nil {
				continue
			}
			out = append(out, m)
		}
		sortNetworks(out)
		return nil
	})
	return out, err
}

func (p *Provider) GetNetwork(_ context.Context, id string) (types.Network, error) {
	var m types.Network
	err := p.withConn(func(c *libvirt.Connect) error {
		n, err := c.LookupNetworkByName(id)
		if err != nil {
			return hypervisor.ErrNetworkNotFound
		}
		m, err = networkToModel(n)
		return err
	})
	return m, err
}

func (p *Provider) StartNetwork(_ context.Context, id string) (types.Network, error) {
	err := p.withConn(func(c *libvirt.Connect) error {
		n, err := c.LookupNetworkByName(id)
		if err != nil {
			return hypervisor.ErrNetworkNotFound
		}
		active, err := n.IsActive()
		if err != nil {
			return err
		}
		if active {
			return fmt.Errorf("%w: network is already active", hypervisor.ErrInvalidNetworkState)
		}
		return n.Create()
	})
	if err != nil {
		return types.Network{}, err
	}
	return p.GetNetwork(context.Background(), id)
}

func (p *Provider) StopNetwork(_ context.Context, id string) (types.Network, error) {
	err := p.withConn(func(c *libvirt.Connect) error {
		n, err := c.LookupNetworkByName(id)
		if err != nil {
			return hypervisor.ErrNetworkNotFound
		}
		active, err := n.IsActive()
		if err != nil {
			return err
		}
		if !active {
			return fmt.Errorf("%w: network is already inactive", hypervisor.ErrInvalidNetworkState)
		}
		return n.Destroy()
	})
	if err != nil {
		return types.Network{}, err
	}
	return p.GetNetwork(context.Background(), id)
}

func networkToModel(net *libvirt.Network) (types.Network, error) {
	name, err := net.GetName()
	if err != nil {
		return types.Network{}, err
	}
	active, err := net.IsActive()
	if err != nil {
		return types.Network{}, err
	}
	autostart, err := net.GetAutostart()
	if err != nil {
		autostart = false
	}

	state := types.NetworkStateInactive
	if active {
		state = types.NetworkStateActive
	}

	var nx networkXML
	if xmlStr, err := net.GetXMLDesc(0); err == nil {
		_ = xml.Unmarshal([]byte(xmlStr), &nx)
	}

	dhcpPresent := nx.IP.DHCP != nil
	mode := networkForwardMode(nx.Forward.Mode)
	m := types.Network{
		Id:          name,
		Name:        name,
		State:       state,
		Autostart:   autostart,
		DhcpEnabled: dhcpPresent,
		Mode:        &mode,
	}
	if nx.Bridge.Name != "" {
		m.Bridge = &nx.Bridge.Name
	}
	if nx.IP.Address != "" {
		ip := nx.IP.Address
		m.IpAddress = &ip
		prefix := nx.IP.Prefix
		m.IpPrefix = &prefix
	}
	if nx.IP.Domain.Name != "" {
		domain := nx.IP.Domain.Name
		m.DomainName = &domain
	}
	return m, nil
}

func sortNetworks(nets []types.Network) {
	for i := 1; i < len(nets); i++ {
		for j := i; j > 0 && nets[j].Id < nets[j-1].Id; j-- {
			nets[j], nets[j-1] = nets[j-1], nets[j]
		}
	}
}

// networkForwardMode maps a libvirt forward mode to our contract enum. An
// empty forward element means an isolated network.
func networkForwardMode(mode string) types.NetworkMode {
	switch mode {
	case "nat", "bridge":
		return types.NetworkMode(mode)
	case "route", "open", "vepa", "passthrough", "hostdev":
		// other forwarding modes exist; report them as their closest cousin
		return types.NetworkModeNat
	}
	return types.NetworkModeIsolated
}

// CreateNetwork defines and starts a virtual network.
func (p *Provider) CreateNetwork(_ context.Context, req types.NetworkCreate) (types.Network, error) {
	xmlDef, err := networkCreateXML(req)
	if err != nil {
		return types.Network{}, err
	}
	err = p.withConn(func(c *libvirt.Connect) error {
		if _, err := c.LookupNetworkByName(req.Name); err == nil {
			return hypervisor.ErrNetworkAlreadyExists
		}
		net, err := c.NetworkDefineXML(xmlDef)
		if err != nil {
			return err
		}
		if err := net.Create(); err != nil {
			_ = net.Undefine()
			return err
		}
		autostart := req.Autostart == nil || *req.Autostart
		_ = net.SetAutostart(autostart) // non-fatal on failure
		return nil
	})
	if err != nil {
		return types.Network{}, err
	}
	return p.GetNetwork(context.Background(), req.Name)
}

// cidrPattern accepts dotted-quad/prefix subnets (e.g. 192.168.100.0/24).
var cidrPattern = regexp.MustCompile(`^([0-9]{1,3}\.){3}[0-9]{1,3}/([0-9]|[12][0-9]|3[0-2])$`)

// ifaceNamePattern is an allowlist for interface names (host bridges).
var ifaceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,15}$`)

// networkCreateXML builds the libvirt network XML for the three supported
// modes. Validation errors are surfaced as ErrInvalidNetworkState so the API
// layer maps them to a client-facing 4xx.
func networkCreateXML(req types.NetworkCreate) (string, error) {
	switch req.Mode {
	case types.NetworkCreateModeBridge:
		if req.BridgeName == nil || !ifaceNamePattern.MatchString(*req.BridgeName) {
			return "", fmt.Errorf("%w: bridgeName is required (and must be a valid interface name) for bridge networks", hypervisor.ErrInvalidNetworkState)
		}
		return fmt.Sprintf(`<network>
  <name>%s</name>
  <forward mode="bridge"/>
  <bridge name="%s"/>
</network>`, xmlEscape(req.Name), xmlEscape(*req.BridgeName)), nil

	case types.NetworkCreateModeNat, types.NetworkCreateModeIsolated:
		if req.Cidr == nil || !cidrPattern.MatchString(*req.Cidr) {
			return "", fmt.Errorf("%w: cidr is required (e.g. 192.168.100.0/24) for nat/isolated networks", hypervisor.ErrInvalidNetworkState)
		}
		_, ipNet, err := net.ParseCIDR(*req.Cidr)
		if err != nil {
			return "", fmt.Errorf("%w: invalid cidr: %v", hypervisor.ErrInvalidNetworkState, err)
		}
		gw, last := subnetFirstAndLastHost(ipNet)
		if gw == nil {
			return "", fmt.Errorf("%w: cidr must be an IPv4 subnet of at least /30", hypervisor.ErrInvalidNetworkState)
		}
		prefix, _ := ipNet.Mask.Size()

		dhcp := ""
		if req.DhcpEnabled == nil || *req.DhcpEnabled {
			dhcp = fmt.Sprintf(`
    <dhcp>
      <range start="%s" end="%s"/>
    </dhcp>`, gw.String(), last.String())
		}

		forward := ""
		if req.Mode == types.NetworkCreateModeNat {
			forward = `<forward mode="nat"/>`
		}
		return fmt.Sprintf(`<network>
  <name>%s</name>
  %s
  <domain name="%s"/>
  <ip address="%s" prefix="%d">%s
  </ip>
</network>`, xmlEscape(req.Name), forward, xmlEscape(req.Name), gw.String(), prefix, dhcp), nil
	}
	return "", fmt.Errorf("%w: unsupported network mode %q", hypervisor.ErrInvalidNetworkState, req.Mode)
}

// subnetFirstAndLastHost returns the first usable host address (used as the
// network gateway) and the last usable host address of the subnet.
func subnetFirstAndLastHost(ipNet *net.IPNet) (first, last net.IP) {
	network := ipNet.IP.To4()
	if network == nil {
		return nil, nil
	}
	size := len(network)
	first = make(net.IP, size)
	last = make(net.IP, size)
	for i := range network {
		first[i] = network[i]&ipNet.Mask[i] | 1 // network+1 (gateway)
		last[i] = network[i]&ipNet.Mask[i] | ^ipNet.Mask[i]
	}
	// last host = broadcast-1
	last = addToIP(last, -1)
	return first, last
}

func addToIP(ip net.IP, delta int32) net.IP {
	out := make(net.IP, len(ip))
	copy(out, ip)
	carry := delta
	for i := len(out) - 1; i >= 0 && carry != 0; i-- {
		v := int32(out[i]) + carry
		out[i] = byte(v)
		carry = v >> 8
	}
	return out
}

// hostInterfaceXML is the subset of host interface XML we consume.
type hostInterfaceXML struct {
	Type string `xml:"type,attr"`
}

// ListHostBridges returns the Linux bridges configured on the host. Bridges
// are host configuration (netplan/NetworkManager); UltraV only detects them.
func (p *Provider) ListHostBridges(_ context.Context) ([]types.HostBridge, error) {
	var out []types.HostBridge
	err := p.withConn(func(c *libvirt.Connect) error {
		active, err := c.ListInterfaces()
		if err != nil {
			return err
		}
		defined, err := c.ListDefinedInterfaces()
		if err != nil {
			return err
		}
		activeSet := make(map[string]bool, len(active))
		for _, n := range active {
			activeSet[n] = true
		}
		all := append(append([]string{}, active...), defined...)
		seen := make(map[string]bool, len(all))
		for _, name := range all {
			if seen[name] {
				continue
			}
			seen[name] = true
			iface, err := c.LookupInterfaceByName(name)
			if err != nil {
				continue
			}
			var ix hostInterfaceXML
			xmlStr, err := iface.GetXMLDesc(0)
			if err != nil || xml.Unmarshal([]byte(xmlStr), &ix) != nil {
				continue
			}
			if ix.Type == "bridge" {
				out = append(out, types.HostBridge{Name: name, Active: activeSet[name]})
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return nil
	})
	return out, err
}
