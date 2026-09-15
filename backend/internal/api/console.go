package api

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

// writeTimeout bounds a single WebSocket frame write to the browser.
const writeTimeout = 10 * time.Second

// bootstrapTimeout bounds the RFB handshake phase of the console proxy.
const bootstrapTimeout = 10 * time.Second

// handleVMConsole upgrades GET /api/v1/vms/{id}/console to a WebSocket and
// proxies binary frames to/from the VM's graphical console (VNC/RFB). The
// browser side is a noVNC client.
func (s *Server) handleVMConsole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	conn, err := s.provider.OpenVMConsole(r.Context(), id)
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	defer conn.Close()

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// noVNC requests the "binary" subprotocol; browsers abort the
		// handshake if the server does not echo a selected subprotocol back.
		Subprotocols: []string{"binary"},
		// Same-origin only: the UI is served from this host, and phase-1
		// has no cross-origin console consumers.
		OriginPatterns: nil,
	})
	if err != nil {
		return
	}
	defer ws.Close(websocket.StatusNormalClosure, "")

	// Framebuffer bootstrap: QEMU <= 4.2 never sends the base framebuffer to
	// a newly connected VNC client (its dirty map starts empty), which leaves
	// noVNC with a black screen until the guest redraws. Mitigate by MITM-ing
	// the fixed-size RFB handshake and injecting a full framebuffer update
	// captured with a QEMU monitor screendump. Best effort: on any error the
	// connection is dropped and noVNC retries (ending up as a plain proxy).
	if dumper, ok := s.provider.(hypervisor.ScreenDumper); ok {
		leftover, err := s.bootstrapConsoleFrame(ws, conn, dumper, r.Context(), id)
		if err != nil {
			s.log.Warn("console framebuffer bootstrap failed",
				"vm", id, "err", err)
			return
		}
		if len(leftover) > 0 {
			if _, err := conn.Write(leftover); err != nil {
				return
			}
		}
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// VNC server -> browser
	go func() {
		defer cancel()
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				wctx, wcancel := context.WithTimeout(ctx, writeTimeout)
				if werr := ws.Write(wctx, websocket.MessageBinary, buf[:n]); werr != nil {
					wcancel()
					return
				}
				wcancel()
			}
			if err != nil {
				return
			}
		}
	}()

	// browser -> VNC server
	for {
		mt, reader, err := ws.Reader(ctx)
		if err != nil {
			return
		}
		if mt == websocket.MessageText {
			ws.Close(websocket.StatusUnsupportedData, "binary frames required")
			return
		}
		if _, err := io.Copy(conn, reader); err != nil && !errors.Is(err, io.EOF) {
			return
		}
	}
}

// ---------- RFB handshake bootstrap ----------

// rfbWS reads exact byte counts from the WebSocket (client) side of the
// console during the handshake phase. Excess bytes from coalesced frames are
// buffered and returned on subsequent reads.
type rfbWS struct {
	ws  *websocket.Conn
	ctx context.Context
	r   io.Reader
	buf []byte
}

func (s *rfbWS) readFull(n int) ([]byte, error) {
	for len(s.buf) < n {
		if s.r == nil {
			mt, r, err := s.ws.Reader(s.ctx)
			if err != nil {
				return nil, err
			}
			if mt != websocket.MessageBinary {
				return nil, errors.New("unexpected text frame during handshake")
			}
			s.r = r
		}
		tmp := make([]byte, 4096)
		m, err := s.r.Read(tmp)
		s.buf = append(s.buf, tmp[:m]...)
		if err != nil {
			s.r = nil // message exhausted (io.EOF) or read error
		}
	}
	out := s.buf[:n]
	s.buf = s.buf[n:]
	return out, nil
}

func (s *rfbWS) write(b []byte) error {
	wctx, cancel := context.WithTimeout(s.ctx, writeTimeout)
	defer cancel()
	return s.ws.Write(wctx, websocket.MessageBinary, b)
}

// leftover reports bytes read ahead from the WebSocket (a client message may
// carry SetPixelFormat + SetEncodings + FBURQ coalesced in a single frame).
// They must be forwarded to the VNC server after the handshake.
func (s *rfbWS) leftover() []byte { return s.buf }

// vncSource reads exact byte counts from the VNC (server) side.
type vncSource struct {
	r io.Reader
}

func (s *vncSource) readFull(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(s.r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// rfbClient is the browser side of the handshake (implemented by rfbWS over
// the WebSocket, and by tests over plain pipes).
type rfbClient interface {
	readFull(n int) ([]byte, error)
	write(b []byte) error
}

// bootstrapConsoleFrame transparently relays the fixed-size RFB handshake
// between the browser (ws) and the VNC server (conn). Once the client has
// negotiated its pixel format, it captures a screendump and injects a full
// framebuffer update so noVNC paints the current screen immediately. The
// remaining bytes flow through the plain proxy loop afterwards.
func (s *Server) bootstrapConsoleFrame(ws *websocket.Conn, conn net.Conn, dumper hypervisor.ScreenDumper, ctx context.Context, id string) ([]byte, error) {
	// Bound the handshake phase.
	conn.SetDeadline(time.Now().Add(bootstrapTimeout))
	defer conn.SetDeadline(time.Time{})

	c := &rfbWS{ws: ws, ctx: ctx}
	err := runRFBBootstrap(c, conn, func() ([]byte, error) { return dumper.ScreenDumpVM(ctx, id) })
	// Bytes read ahead from the WebSocket belong to the VNC server: dropping
	// them would desync the session (e.g. a coalesced SetPixelFormat +
	// SetEncodings + FBURQ frame).
	return c.leftover(), err
}

// runRFBBootstrap performs the RFB handshake relaying and injects the
// screendump framebuffer. cli is the browser side; srvConn the VNC side.
func runRFBBootstrap(cli rfbClient, srvConn net.Conn, dump func() ([]byte, error)) error {
	c := cli
	v := &vncSource{r: srvConn}
	srv := func(b []byte) error { _, err := srvConn.Write(b); return err }

	// ProtocolVersion: server then client (12 bytes each, "RFB xxx.yyy\n").
	sv, err := v.readFull(12)
	if err != nil {
		return err
	}
	if err = c.write(sv); err != nil {
		return err
	}
	cv, err := c.readFull(12)
	if err != nil {
		return err
	}
	if err = srv(cv); err != nil {
		return err
	}

	// Security types: 1 byte count + types; client picks one (None = 1).
	st, err := v.readFull(1)
	if err != nil {
		return err
	}
	n := int(st[0])
	rest, err := v.readFull(n)
	if err != nil {
		return err
	}
	if err = c.write(append(st, rest...)); err != nil {
		return err
	}
	ctype, err := c.readFull(1)
	if err != nil {
		return err
	}
	if err = srv(ctype); err != nil {
		return err
	}
	if ctype[0] == 1 {
		res, err := v.readFull(4)
		if err != nil {
			return err
		}
		if err = c.write(res); err != nil {
			return err
		}
	} else {
		// Non-anonymous security (TLS/VNC auth): out of scope — plain proxy.
		return errors.New("non-none security handshake; skipping framebuffer bootstrap")
	}

	// ClientInitialization (shared flag), then ServerInit:
	// 2 w, 2 h, 16 pixfmt, 4 name-length, name.
	cinit, err := c.readFull(1)
	if err != nil {
		return err
	}
	if err = srv(cinit); err != nil {
		return err
	}
	si, err := v.readFull(24)
	if err != nil {
		return err
	}
	w := binary.BigEndian.Uint16(si[0:2])
	h := binary.BigEndian.Uint16(si[2:4])
	nameLen := binary.BigEndian.Uint32(si[20:24])
	name, err := v.readFull(int(nameLen))
	if err != nil {
		return err
	}
	if err = c.write(si); err != nil {
		return err
	}
	if err = c.write(name); err != nil {
		return err
	}

	// ClientSetPixelFormat: type 0 + 3 pad + 16 byte pixfmt.
	pf, err := c.readFull(20)
	if err != nil {
		return err
	}
	if pf[0] != 0 {
		return errors.New("unexpected client message; skipping framebuffer bootstrap")
	}
	if err = srv(pf); err != nil {
		return err
	}

	// Only inject when the client format is directly convertible (16 bpp,
	// little endian). The patched noVNC always negotiates RGB565 LE.
	bpp := pf[4]
	bigEndian := pf[6]
	if bpp != 16 || bigEndian != 0 {
		return nil // nothing to inject; plain proxy from here
	}
	rMax := binary.BigEndian.Uint16(pf[8:10])
	gMax := binary.BigEndian.Uint16(pf[10:12])
	bMax := binary.BigEndian.Uint16(pf[12:14])
	rShift, gShift, bShift := pf[14], pf[15], pf[16]

	ppm, err := dump()
	if err != nil {
		return fmt.Errorf("screendump: %w", err)
	}
	raw, rw, rh, ok := ppmToFramebuffer(ppm, int(w), int(h), rMax, gMax, bMax, rShift, gShift, bShift)
	if !ok {
		return errors.New("screendump format mismatch; skipping framebuffer bootstrap")
	}

	// FramebufferUpdate: type 0, pad 1, nrects 1, rect {x,y,w,h,enc=Raw}.
	fbu := make([]byte, 4+12+len(raw))
	fbu[0] = 0
	fbu[2] = 0
	fbu[3] = 1
	binary.BigEndian.PutUint16(fbu[4:6], 0)
	binary.BigEndian.PutUint16(fbu[6:8], 0)
	binary.BigEndian.PutUint16(fbu[8:10], uint16(rw))
	binary.BigEndian.PutUint16(fbu[10:12], uint16(rh))
	copy(fbu[16:], raw)
	if err := c.write(fbu); err != nil {
		return err
	}
	return nil
}

// ppmToFramebuffer converts a P6 PPM (RGB888) image to raw framebuffer bytes
// in the client's 16 bpp little-endian pixel format. Returns ok=false when the
// dump dimensions do not match the negotiated framebuffer.
func ppmToFramebuffer(ppm []byte, fw, fh int, rMax, gMax, bMax uint16, rShift, gShift, bShift byte) ([]byte, int, int, bool) {
	// Parse "P6" header: width, height, maxval, single whitespace separator.
	if len(ppm) < 2 || ppm[0] != 'P' || ppm[1] != '6' {
		return nil, 0, 0, false
	}
	pos := 2
	fields := make([]int, 0, 3)
	for len(fields) < 3 {
		// skip whitespace and comments
		for pos < len(ppm) {
			ch := ppm[pos]
			if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
				pos++
				continue
			}
			if ch == '#' {
				for pos < len(ppm) && ppm[pos] != '\n' {
					pos++
				}
				continue
			}
			break
		}
		start := pos
		for pos < len(ppm) && ppm[pos] >= '0' && ppm[pos] <= '9' {
			pos++
		}
		if pos == start {
			return nil, 0, 0, false
		}
		v, err := strconv.Atoi(string(ppm[start:pos]))
		if err != nil {
			return nil, 0, 0, false
		}
		fields = append(fields, v)
	}
	pos++ // single whitespace after maxval
	w, h, maxval := fields[0], fields[1], fields[2]
	if w != fw || h != fh || maxval != 255 || len(ppm)-pos < w*h*3 {
		return nil, 0, 0, false
	}

	out := make([]byte, 0, w*h*2)
	for i := 0; i < w*h*3; i += 3 {
		r := uint32(ppm[pos+i]) * uint32(rMax) / 255
		g := uint32(ppm[pos+i+1]) * uint32(gMax) / 255
		b := uint32(ppm[pos+i+2]) * uint32(bMax) / 255
		v := uint16(r)<<rShift | uint16(g)<<gShift | uint16(b)<<bShift
		out = append(out, byte(v), byte(v>>8)) // little endian
	}
	return out, w, h, true
}
