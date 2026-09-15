package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

// pipeClient implements rfbClient over a net.Conn, speaking like noVNC.
type pipeClient struct {
	conn net.Conn
}

func (p *pipeClient) readFull(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(p.conn, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (p *pipeClient) write(b []byte) error {
	_, err := p.conn.Write(b)
	return err
}

// fakeVNC emulates the QEMU side: handshake, then verifies the client bytes
// forwarded by the proxy, then replies with a small real FBU.
type fakeVNC struct {
	conn net.Conn
	w, h uint16
	name string
	t    *testing.T
}

func (f *fakeVNC) run() {
	c := f.conn
	defer c.Close()
	fail := func(err error) { f.t.Logf("fakeVNC: %v", err) }

	if _, err := c.Write([]byte("RFB 003.008\n")); err != nil {
		fail(err)
		return
	}
	if _, err := readN(c, 12); err != nil {
		fail(err)
		return
	}
	if _, err := c.Write([]byte{1, 1}); err != nil { // 1 security type: None
		fail(err)
		return
	}
	if _, err := readN(c, 1); err != nil {
		fail(err)
		return
	}
	if _, err := c.Write([]byte{0, 0, 0, 0}); err != nil { // SecurityResult
		fail(err)
		return
	}
	if _, err := readN(c, 1); err != nil { // ClientInit
		fail(err)
		return
	}
	name := []byte(f.name)
	si := make([]byte, 24)
	binary.BigEndian.PutUint16(si[0:2], f.w)
	binary.BigEndian.PutUint16(si[2:4], f.h)
	binary.BigEndian.PutUint32(si[20:24], uint32(len(name)))
	if _, err := c.Write(si); err != nil {
		fail(err)
		return
	}
	if _, err := c.Write(name); err != nil {
		fail(err)
		return
	}

	// SetPixelFormat(20): forwarded by the bootstrap before injection.
	pf, err := readN(c, 20)
	if err != nil {
		fail(err)
		return
	}
	if pf[0] != 0 || pf[4] != 16 || pf[7] != 1 {
		f.t.Errorf("fakeVNC: corrupted SetPixelFormat: % x", pf)
		return
	}

	// Post-handshake client bytes: SetEncodings + FBURQ forwarded intact.
	want := []byte{2, 0, 0, 2, 0, 0, 0, 1, 0, 0, 0, 0} // SetEncodings [CopyRect, Raw]
	want = append(want, func() []byte {
		fur := make([]byte, 10)
		fur[0] = 3
		binary.BigEndian.PutUint16(fur[6:8], f.w)
		binary.BigEndian.PutUint16(fur[8:10], f.h)
		return fur
	}()...)
	rest := make([]byte, 0, len(want))
	buf := make([]byte, 4096)
	deadline := time.Now().Add(5 * time.Second)
	for len(rest) < len(want) && time.Now().Before(deadline) {
		c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		n, err := c.Read(buf)
		rest = append(rest, buf[:n]...)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			break
		}
	}
	if !bytes.Equal(rest, want) {
		f.t.Errorf("fakeVNC: post-handshake client bytes mismatch:\n got % x\nwant % x", rest, want)
	}
}

func readN(c net.Conn, n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(c, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

type fakeDumper struct{ w, h int }

func (f fakeDumper) ScreenDumpVM(ctx context.Context, id string) ([]byte, error) {
	hdr := fmt.Sprintf("P6\n%d %d\n255\n", f.w, f.h)
	px := make([]byte, f.w*f.h*3)
	for i := range px {
		px[i] = byte(i%251 + 2) // avoid all-zero
	}
	return append([]byte(hdr), px...), nil
}

func TestRunRFBBootstrapByteAccounting(t *testing.T) {
	const w, h = 1024, 768
	// Two pipes: test-client <-> bootstrap <-> fakeVNC.
	testCli, bootCli := net.Pipe()
	bootSrv, fakeSrv := net.Pipe()

	fake := &fakeVNC{conn: fakeSrv, w: w, h: h, name: "QEMU (win11-test)", t: t}
	go fake.run()

	bootstrapDone := make(chan struct{})
	go func() {
		defer close(bootstrapDone)
		dump := func() ([]byte, error) {
			return fakeDumper{w: w, h: h}.ScreenDumpVM(context.Background(), "x")
		}
		if err := runRFBBootstrap(&pipeClient{conn: bootCli}, bootSrv, dump); err != nil {
			t.Errorf("bootstrap: %v", err)
		}
	}()

	cli := &pipeClient{conn: testCli}
	defer testCli.Close()

	// Client side of the handshake (noVNC order).
	t.Log("cli: aguardando server version")
	sv, err := cli.readFull(12)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sv, []byte("RFB 003.008\n")) {
		t.Fatalf("server version %q", sv)
	}
	t.Log("cli: enviando client version")
	if err := cli.write([]byte("RFB 003.008\n")); err != nil {
		t.Fatal(err)
	}
	t.Log("cli: aguardando security types")
	st, err := cli.readFull(1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cli.readFull(int(st[0])); err != nil { // lista de tipos
		t.Fatal(err)
	}
	// Cliente envia o pick ANTES de ler o SecurityResult (ordem RFB).
	if err := cli.write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if _, err := cli.readFull(4); err != nil { // SecurityResult
		t.Fatal(err)
	}
	if err := cli.write([]byte{1}); err != nil { // ClientInit
		t.Fatal(err)
	}
	si, err := cli.readFull(24)
	if err != nil {
		t.Fatal(err)
	}
	nameLen := binary.BigEndian.Uint32(si[20:24])
	if _, err := cli.readFull(int(nameLen)); err != nil {
		t.Fatal(err)
	}

	// SetPixelFormat goes through the bootstrap handshake itself.
	pfDone := make(chan error, 1)
	go func() {
		pf := make([]byte, 20)
		pf[4] = 16
		pf[5] = 16
		pf[7] = 1
		binary.BigEndian.PutUint16(pf[8:10], 31)
		binary.BigEndian.PutUint16(pf[10:12], 63)
		binary.BigEndian.PutUint16(pf[12:14], 31)
		pf[14] = 11
		pf[15] = 5
		pfDone <- cli.write(pf)
	}()

	// The injected FBU must arrive: header 4, rect 12, raw w*h*2.
	hdr, err := cli.readFull(4)
	if err != nil {
		t.Fatal(err)
	}
	if hdr[0] != 0 {
		t.Fatalf("expected injected FBU type 0, got %d", hdr[0])
	}
	if nr := binary.BigEndian.Uint16(hdr[2:4]); nr != 1 {
		t.Fatalf("expected 1 rect, got %d", nr)
	}
	rect, err := cli.readFull(12)
	if err != nil {
		t.Fatal(err)
	}
	rw := binary.BigEndian.Uint16(rect[4:6])
	rh := binary.BigEndian.Uint16(rect[6:8])
	if rw != w || rh != h {
		t.Fatalf("unexpected rect %dx%d", rw, rh)
	}
	if encval := binary.BigEndian.Uint32(rect[8:12]); encval != 0 {
		t.Fatalf("expected Raw encoding, got %d", encval)
	}
	raw, err := cli.readFull(int(w) * int(h) * 2)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(raw, make([]byte, len(raw))) {
		t.Fatal("injected framebuffer is all zero")
	}

	if err := <-pfDone; err != nil {
		t.Fatal(err)
	}

	// Pós-handshake: reconstitui o proxy (produção usa io.Copy nos dois
	// sentidos) para que SetEncodings/FBURQ cheguem ao fakeVNC.
	go func() { _, _ = io.Copy(bootSrv, bootCli) }()

	go func() {
		enc := []byte{2, 0, 0, 2, 0, 0, 0, 1, 0, 0, 0, 0} // SetEncodings [CopyRect, Raw]
		if err := cli.write(enc); err != nil {
			return
		}
		fur := make([]byte, 10)
		fur[0] = 3
		binary.BigEndian.PutUint16(fur[6:8], w)
		binary.BigEndian.PutUint16(fur[8:10], h)
		_ = cli.write(fur)
	}()

	// A verificação de que SetEncodings/FBURQ chegam intactos ao QEMU é
	// feita dentro de fakeVNC.run via t.Errorf.
}
