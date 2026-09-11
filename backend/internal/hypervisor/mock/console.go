package mock

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

// OpenVMConsole returns one end of a pipe served by a minimal RFB (VNC)
// server, so the web console can be exercised in mock mode.
func (p *Provider) OpenVMConsole(_ context.Context, id string) (net.Conn, error) {
	p.mu.Lock()
	st, ok := p.vms[id]
	running := ok && st.state == types.VMStateRunning
	p.mu.Unlock()
	if !ok {
		return nil, hypervisor.ErrVMNotFound
	}
	if !running {
		return nil, fmt.Errorf("%w: console requires a running virtual machine", hypervisor.ErrInvalidVMState)
	}

	client, server := net.Pipe()
	go serveRFB(server, id)
	return client, nil
}

// serveRFB implements just enough of RFB 3.8 for a noVNC client to connect
// and display a static screen. Client messages other than
// FramebufferUpdateRequest are consumed and ignored.
func serveRFB(c net.Conn, name string) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))

	const (
		w = 640
		h = 400
	)
	// --- handshake ---
	if _, err := c.Write([]byte("RFB 003.008\n")); err != nil {
		return
	}
	buf := make([]byte, 12)
	if _, err := io.ReadFull(c, buf); err != nil {
		return
	}
	// security types: [1] = None
	if _, err := c.Write([]byte{1, 1}); err != nil {
		return
	}
	if _, err := io.ReadFull(c, buf[:1]); err != nil {
		return
	}
	var ok32 [4]byte // security result: 0 = OK
	if _, err := c.Write(ok32[:]); err != nil {
		return
	}
	if _, err := io.ReadFull(c, buf[:1]); err != nil { // ClientInit
		return
	}
	// ServerInit: width, height, pixel format (32bpp true colour), name.
	init := make([]byte, 0, 24+len(name))
	init = binary.BigEndian.AppendUint16(init, w)
	init = binary.BigEndian.AppendUint16(init, h)
	init = append(init,
		32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0, // pixel format
	)
	init = binary.BigEndian.AppendUint32(init, uint32(len(name)))
	init = append(init, name...)
	if _, err := c.Write(init); err != nil {
		return
	}
	_ = c.SetDeadline(time.Time{})

	// --- frame loop: answer update requests, skip everything else ---
	// The client may switch our pixel format (SetPixelFormat); re-render
	// pixels in whatever format it asked for (16bpp matters for noVNC,
	// which defaults to it — see the rfb.js patch).
	bpp := 32
	var rmax, gmax, bmax uint16 = 255, 255, 255
	var rshift, gshift, bshift uint8 = 16, 8, 0
	frame := make([]byte, 0, 16+h*w*4)
	buildFrame := func() {
		rgb := func(r, g, b int) uint64 {
			// quantize to the client's maxima and pack by its shifts
			rq := uint64(r) * uint64(rmax) / 255
			gq := uint64(g) * uint64(gmax) / 255
			bq := uint64(b) * uint64(bmax) / 255
			return rq<<rshift | gq<<gshift | bq<<bshift
		}
		bytesPP := bpp / 8
		frame = frame[:0]
		frame = append(frame, 0)                        // FramebufferUpdate
		frame = append(frame, 0)                        // padding
		frame = binary.BigEndian.AppendUint16(frame, 1) // one rectangle
		frame = binary.BigEndian.AppendUint16(frame, 0) // x
		frame = binary.BigEndian.AppendUint16(frame, 0) // y
		frame = binary.BigEndian.AppendUint16(frame, w)
		frame = binary.BigEndian.AppendUint16(frame, h)
		frame = binary.BigEndian.AppendUint32(frame, 0) // raw encoding
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				// subtle gradient so the console looks alive
				c := rgb(0x18+x*0x20/w, 0x1a+y*0x30/h, 0x52)
				for i := bytesPP - 1; i >= 0; i-- {
					frame = append(frame, byte(c>>(8*i)))
				}
			}
		}
	}
	buildFrame()

	msg := make([]byte, 64)
	for {
		if _, err := io.ReadFull(c, msg[:1]); err != nil {
			return
		}
		switch msg[0] {
		case 3: // FramebufferUpdateRequest
			if _, err := io.ReadFull(c, msg[:9]); err != nil {
				return
			}
			if _, err := c.Write(frame); err != nil {
				return
			}
		case 2: // SetEncodings
			var hdr [3]byte
			if _, err := io.ReadFull(c, hdr[:]); err != nil {
				return
			}
			n := binary.BigEndian.Uint16(hdr[1:])
			if _, err := io.CopyN(io.Discard, c, int64(n)*4); err != nil {
				return
			}
		case 0: // SetPixelFormat: honor bpp/maxima/shifts
			var msg19 [19]byte
			if _, err := io.ReadFull(c, msg19[:]); err != nil {
				return
			}
			bpp = int(msg19[3])
			if bpp != 8 && bpp != 16 && bpp != 32 {
				bpp = 32
			}
			rmax = binary.BigEndian.Uint16(msg19[6:8])
			gmax = binary.BigEndian.Uint16(msg19[8:10])
			bmax = binary.BigEndian.Uint16(msg19[10:12])
			rshift = msg19[12]
			gshift = msg19[13]
			bshift = msg19[14]
			buildFrame()
		case 4, 5: // KeyEvent, PointerEvent — ignore
			size := map[byte]int{4: 7, 5: 5}[msg[0]]
			if _, err := io.ReadFull(c, msg[:size]); err != nil {
				return
			}
		case 6: // ClientCutText
			var hdr [7]byte
			if _, err := io.ReadFull(c, hdr[:]); err != nil {
				return
			}
			n := binary.BigEndian.Uint32(hdr[3:])
			if _, err := io.CopyN(io.Discard, c, int64(n)); err != nil {
				return
			}
		default: // QEMU extensions etc. — bail out on unknown traffic
			return
		}
	}
}
