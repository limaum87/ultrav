package mock

import (
	"context"
	"fmt"
	"sync"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

// --- storage pools ---

var poolMu sync.Mutex

func (p *Provider) ListStoragePools(_ context.Context) ([]types.StoragePool, error) {
	poolMu.Lock()
	defer poolMu.Unlock()

	out := make([]types.StoragePool, 0, len(pools))
	for _, pool := range pools {
		out = append(out, poolToModel(pool))
	}
	return out, nil
}

func (p *Provider) GetStoragePool(_ context.Context, id string) (types.StoragePool, error) {
	poolMu.Lock()
	defer poolMu.Unlock()

	for _, pool := range pools {
		if pool.id == id {
			return poolToModel(pool), nil
		}
	}
	return types.StoragePool{}, hypervisor.ErrPoolNotFound
}

// RefreshStoragePool re-syncs usage numbers; in the mock this nudges the
// allocation of the default pool slightly to feel alive.
func (p *Provider) RefreshStoragePool(_ context.Context, id string) (types.StoragePool, error) {
	poolMu.Lock()
	defer poolMu.Unlock()

	for i := range pools {
		if pools[i].id == id {
			if !pools[i].active {
				return types.StoragePool{}, fmt.Errorf("%w: cannot refresh inactive storage pool", hypervisor.ErrInvalidNetworkState)
			}
			return poolToModel(pools[i]), nil
		}
	}
	return types.StoragePool{}, hypervisor.ErrPoolNotFound
}

func poolToModel(src struct {
	id         string
	typeName   string
	active     bool
	autostart  bool
	capacity   int64
	allocation int64
	targetPath string
}) types.StoragePool {
	state := types.StoragePoolStateInactive
	if src.active {
		state = types.StoragePoolStateActive
	}
	poolType := types.StoragePoolType(src.typeName)
	target := src.targetPath
	autostart := src.autostart
	return types.StoragePool{
		Id:              src.id,
		Name:            src.id,
		Type:            poolType,
		State:           state,
		Autostart:       &autostart,
		CapacityBytes:   src.capacity,
		AllocationBytes: src.allocation,
		AvailableBytes:  src.capacity - src.allocation,
		TargetPath:      &target,
	}
}

// --- networks ---

func (p *Provider) ListNetworks(_ context.Context) ([]types.Network, error) {
	poolMu.Lock()
	defer poolMu.Unlock()

	out := make([]types.Network, 0, len(networks))
	for _, n := range networks {
		out = append(out, networkToModel(n))
	}
	return out, nil
}

func (p *Provider) GetNetwork(_ context.Context, id string) (types.Network, error) {
	n, err := p.networkLocked(id)
	if err != nil {
		return types.Network{}, err
	}
	return networkToModel(*n), nil
}

func (p *Provider) StartNetwork(_ context.Context, id string) (types.Network, error) {
	n, err := p.networkLocked(id)
	if err != nil {
		return types.Network{}, err
	}
	if n.active {
		return types.Network{}, fmt.Errorf("%w: network is already active", hypervisor.ErrInvalidNetworkState)
	}
	n.active = true
	return networkToModel(*n), nil
}

func (p *Provider) StopNetwork(_ context.Context, id string) (types.Network, error) {
	n, err := p.networkLocked(id)
	if err != nil {
		return types.Network{}, err
	}
	if !n.active {
		return types.Network{}, fmt.Errorf("%w: network is already inactive", hypervisor.ErrInvalidNetworkState)
	}
	n.active = false
	return networkToModel(*n), nil
}

// networkLocked looks a network up; callers must hold poolMu.
func (p *Provider) networkLocked(id string) (*struct {
	id        string
	active    bool
	autostart bool
	bridge    string
	ip        string
	prefix    int
	dhcp      bool
	domain    string
}, error) {
	for i := range networks {
		if networks[i].id == id {
			return &networks[i], nil
		}
	}
	return nil, hypervisor.ErrNetworkNotFound
}

func networkToModel(src struct {
	id        string
	active    bool
	autostart bool
	bridge    string
	ip        string
	prefix    int
	dhcp      bool
	domain    string
}) types.Network {
	state := types.NetworkStateInactive
	if src.active {
		state = types.NetworkStateActive
	}
	return types.Network{
		Id:          src.id,
		Name:        src.id,
		State:       state,
		Autostart:   src.autostart,
		Bridge:      &src.bridge,
		IpAddress:   &src.ip,
		IpPrefix:    &src.prefix,
		DhcpEnabled: src.dhcp,
		DomainName:  &src.domain,
	}
}
