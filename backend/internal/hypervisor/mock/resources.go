package mock

// Storage pools and virtual networks simulated by the mock provider.

var pools = []struct {
	id          string
	typeName    string
	active      bool
	autostart   bool
	capacity    int64
	allocation  int64
	targetPath  string
}{
	{id: "default", typeName: "dir", active: true, autostart: true,
		capacity: 2 * 1024 * 1024 * 1024 * 1024, allocation: 720 * 1024 * 1024 * 1024,
		targetPath: "/var/lib/libvirt/images"},
	{id: "backups", typeName: "dir", active: true, autostart: true,
		capacity: 4 * 1024 * 1024 * 1024 * 1024, allocation: 96 * 1024 * 1024 * 1024,
		targetPath: "/srv/backups"},
	{id: "iso", typeName: "dir", active: false, autostart: false,
		capacity: 0, allocation: 0,
		targetPath: "/var/lib/libvirt/iso"},
}

var networks = []struct {
	id        string
	active    bool
	autostart bool
	bridge    string
	ip        string
	prefix    int
	dhcp      bool
	domain    string
}{
	{id: "default", active: true, autostart: true, bridge: "virbr0", ip: "10.0.0.1", prefix: 24, dhcp: true, domain: "lan.ultrav.internal"},
	{id: "mgmt", active: false, autostart: false, bridge: "virbr1", ip: "10.99.0.1", prefix: 24, dhcp: false, domain: "mgmt.ultrav.internal"},
}
