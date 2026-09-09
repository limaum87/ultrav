package libvirt

import (
	"context"
	"encoding/xml"
	"fmt"

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
	IP struct {
		Address string `xml:"address,attr"`
		Prefix  int    `xml:"prefix,attr"`
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
	m := types.Network{
		Id:          name,
		Name:        name,
		State:       state,
		Autostart:   autostart,
		DhcpEnabled: dhcpPresent,
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
