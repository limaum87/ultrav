package libvirt

import (
	"context"
	encoding_xml "encoding/xml"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	libvirt "libvirt.org/go/libvirt"

	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

// graphicsXML is the <graphics> subset of a domain XML (runtime form).
type graphicsXML struct {
	Graphics []struct {
		Type    string `xml:"type,attr"`
		Socket  string `xml:"socket,attr"`
		Listens []struct {
			Type   string `xml:"type,attr"`
			Socket string `xml:"socket,attr"`
		} `xml:"listen"`
	} `xml:"devices>graphics"`
}

// vncSocketPath extracts the runtime unix socket path of the VM's VNC server,
// or "" when VNC is not unix-socket-backed. The runtime `socket` attribute is
// filled in by GetXMLDesc for running domains.
func vncSocketPath(dom *libvirt.Domain) (string, error) {
	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		return "", err
	}
	if !strings.Contains(xmlDesc, "<graphics") {
		return "", hypervisor.ErrConsoleUnavailable
	}
	var gx graphicsXML
	if err := encoding_xml.Unmarshal([]byte(xmlDesc), &gx); err != nil {
		return "", err
	}
	for _, g := range gx.Graphics {
		if g.Type != "vnc" {
			continue
		}
		if g.Socket != "" {
			return g.Socket, nil
		}
		for _, l := range g.Listens {
			if l.Type == "socket" && l.Socket != "" {
				return l.Socket, nil
			}
		}
	}
	return "", nil // VNC present, but not on a unix socket
}

// OpenVMConsole returns a live connection to the VM's graphical console
// (VNC/RFB). Preferred path: dial the VNC unix socket directly (works even
// where virDomainOpenGraphicsFD is denied, e.g. by QEMU sandboxing). Falls
// back to virDomainOpenGraphicsFD for TCP-listening consoles.
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

	sockPath, err := vncSocketPath(dom)
	if err != nil {
		return nil, err
	}
	if sockPath != "" {
		conn, err := net.DialTimeout("unix", sockPath, 5*time.Second)
		if err == nil {
			return conn, nil
		}
		// Fall through to OpenGraphicsFD on dial failure.
	}

	file, err := dom.OpenGraphicsFD(0, libvirt.DOMAIN_OPEN_GRAPHICS_SKIPAUTH)
	if err != nil {
		return nil, fmt.Errorf("cannot open graphics console for %s: %w", id, err)
	}
	return fileConn{file}, nil
}

// fileConn adapts an *os.File (e.g. a socket fd from OpenGraphicsFD) to
// net.Conn. Addresses are empty; deadlines and reads/writes pass through.
type fileConn struct{ f *os.File }

func (c fileConn) Read(b []byte) (int, error)         { return c.f.Read(b) }
func (c fileConn) Write(b []byte) (int, error)        { return c.f.Write(b) }
func (c fileConn) Close() error                       { return c.f.Close() }
func (c fileConn) LocalAddr() net.Addr                { return fdAddr(0) }
func (c fileConn) RemoteAddr() net.Addr               { return fdAddr(0) }
func (c fileConn) SetDeadline(t time.Time) error      { return c.f.SetDeadline(t) }
func (c fileConn) SetReadDeadline(t time.Time) error  { return c.f.SetReadDeadline(t) }
func (c fileConn) SetWriteDeadline(t time.Time) error { return c.f.SetWriteDeadline(t) }

type fdAddr int

func (a fdAddr) Network() string { return "fd" }
func (a fdAddr) String() string  { return "libvirt-graphics-fd" }
