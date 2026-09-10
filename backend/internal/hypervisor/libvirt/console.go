package libvirt

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	libvirt "libvirt.org/go/libvirt"

	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

// OpenVMConsole opens the VM's primary graphical console (VNC/SPICE) via
// virDomainOpenGraphicsFD and returns it as a net.Conn. The caller owns the
// connection. SKIPAUTH is safe because the socket is only reachable by this
// process; authentication is the API layer's responsibility.
func (p *Provider) OpenVMConsole(_ context.Context, id string) (net.Conn, error) {
	c, err := p.connect()
	if err != nil {
		return nil, err
	}
	dom, err := c.LookupDomainByName(id)
	if err != nil {
		return nil, hypervisor.ErrVMNotFound
	}
	state, _, err := dom.GetState()
	if err != nil {
		return nil, err
	}
	if state != libvirt.DOMAIN_RUNNING && state != libvirt.DOMAIN_BLOCKED {
		return nil, fmt.Errorf("%w: console requires a running virtual machine", hypervisor.ErrInvalidVMState)
	}
	// Fail with a clear error when the domain simply has no graphics device.
	xml, err := dom.GetXMLDesc(0)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(xml, "<graphics") {
		return nil, hypervisor.ErrConsoleUnavailable
	}
	// The file descriptor must outlive this callback's scope; it is owned by
	// the returned net.Conn and closed by the caller.
	file, err := dom.OpenGraphicsFD(0, libvirt.DOMAIN_OPEN_GRAPHICS_SKIPAUTH)
	if err != nil {
		return nil, fmt.Errorf("cannot open graphics console for %s: %w", id, err)
	}
	return fileConn{file}, nil
}

// fileConn adapts an *os.File (e.g. a socket fd from OpenGraphicsFD) to
// net.Conn. Addresses are empty; deadlines and reads/writes pass through.
type fileConn struct{ f *os.File }

func (c fileConn) Read(b []byte) (int, error)  { return c.f.Read(b) }
func (c fileConn) Write(b []byte) (int, error) { return c.f.Write(b) }
func (c fileConn) Close() error                { return c.f.Close() }
func (c fileConn) LocalAddr() net.Addr         { return fdAddr(0) }
func (c fileConn) RemoteAddr() net.Addr        { return fdAddr(0) }
func (c fileConn) SetDeadline(t time.Time) error      { return c.f.SetDeadline(t) }
func (c fileConn) SetReadDeadline(t time.Time) error  { return c.f.SetReadDeadline(t) }
func (c fileConn) SetWriteDeadline(t time.Time) error { return c.f.SetWriteDeadline(t) }

type fdAddr int

func (a fdAddr) Network() string { return "fd" }
func (a fdAddr) String() string  { return "libvirt-graphics-fd" }

