package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// writeTimeout bounds a single WebSocket frame write to the browser.
const writeTimeout = 10 * time.Second


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
		// Same-origin only: the UI is served from this host, and phase-1
		// has no cross-origin console consumers.
		OriginPatterns: nil,
	})
	if err != nil {
		return
	}
	defer ws.Close(websocket.StatusNormalClosure, "")

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
