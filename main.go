// Command gl-tftpd is a small, dependency-free TFTP server (RFC 1350, octet
// mode) that serves a directory over UDP with sandboxed, optionally writable
// file access.
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

// version is set at release build time via -ldflags "-X main.version=vX.Y.Z".
var version string

// TFTP opcodes (RFC 1350)
const (
	opRRQ   = 1
	opWRQ   = 2
	opDATA  = 3
	opACK   = 4
	opERROR = 5
)

// TFTP error codes (RFC 1350)
const (
	errNotDefined      = 0
	errFileNotFound    = 1
	errAccessViolation = 2
	errDiskFull        = 3
	errIllegalOp       = 4
	errUnknownTID      = 5
	errFileExists      = 6
)

const (
	blockSize = 512
	timeout   = 3 * time.Second
	maxRetry  = 5
	// firstBlockRetries caps retransmissions of the first DATA block. A spoofed
	// (reflection) client never ACKs, so sending the first block only once stops
	// the server from amplifying traffic toward a forged source address. A real
	// client that loses the first block just retransmits its request to restart.
	firstBlockRetries = 1
)

var (
	// rootDir is the absolute path of the directory being served (for logging)
	rootDir string
	// rootFS is a sandboxed handle to rootDir: all file access goes through it,
	// so requests cannot escape the directory via ".." or symlinks
	rootFS *os.Root
	// allowWrite enables handling of WRQ (upload) requests
	allowWrite bool
	// maxWriteBytes caps the size of an uploaded file (0 = unlimited)
	maxWriteBytes int64
)

// main sets up the TFTP server and listens for incoming requests
func main() {
	addr := flag.String("addr", ":69", "listen address")
	flag.StringVar(&rootDir, "root", ".", "root directory to serve")
	flag.BoolVar(&allowWrite, "writable", false, "allow clients to upload files (WRQ)")
	flag.Int64Var(&maxWriteBytes, "max-write-bytes", 1<<30, "maximum bytes accepted per upload (0 = unlimited)")
	maxSessions := flag.Int("max-sessions", 256, "maximum concurrent transfers (excess requests are dropped)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("gl-tftpd", resolveVersion())
		return
	}

	// Ensure the root directory is an absolute path
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		log.Fatal(err)
	}
	rootDir = abs

	// Open a sandboxed handle to the root. All subsequent file access is scoped
	// to this directory: os.Root refuses paths that traverse ".." or follow a
	// symlink out of the tree, closing the traversal/symlink-escape hole.
	rootFS, err = os.OpenRoot(rootDir)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = rootFS.Close() }()

	// Resolve the UDP address and start listening for incoming TFTP requests
	udpAddr, err := net.ResolveUDPAddr("udp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	log.Printf("gl-tftpd %s serving %s on %s (writable=%v)", resolveVersion(), rootDir, *addr, allowWrite)

	// Cap concurrent transfers so a packet flood can't exhaust goroutines or file
	// descriptors — each session opens its own UDP socket.
	sem := make(chan struct{}, *maxSessions)

	// Read incoming packets in a loop and handle each request in its own goroutine
	buf := make([]byte, 1024)
	for {
		n, client, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Println("read:", err)
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		select {
		case sem <- struct{}{}:
			go func() {
				defer func() { <-sem }()
				handle(client, pkt)
			}()
		default:
			// At capacity: drop silently. A legitimate client retransmits;
			// replying with an error would just add another reflection vector.
		}
	}
}

// resolveVersion returns the release version injected at build time, falling
// back to the module version (for `go install`) and finally "dev".
func resolveVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "dev"
}

// handle processes a single TFTP request, dispatching to the read or write path
func handle(client *net.UDPAddr, pkt []byte) {
	if len(pkt) < 4 {
		return
	}
	op := binary.BigEndian.Uint16(pkt[:2])

	// Each session gets its own ephemeral UDP socket (TID)
	sess, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		log.Println("session socket:", err)
		return
	}
	defer func() { _ = sess.Close() }()

	filename, mode, err := parseRequest(pkt[2:])
	if err != nil {
		sendError(sess, client, errNotDefined, err.Error())
		return
	}
	if mode != "octet" {
		sendError(sess, client, errNotDefined, "only octet mode supported")
		return
	}

	// Normalize to a root-relative name
	// rootFS enforces the actual boundary
	name := relName(filename)

	switch op {
	case opRRQ:
		log.Printf("RRQ %s from %s", filename, client)
		serveRead(sess, client, name)
	case opWRQ:
		if !allowWrite {
			sendError(sess, client, errAccessViolation, "writes are disabled")
			return
		}
		log.Printf("WRQ %s from %s", filename, client)
		serveWrite(sess, client, name)
	default:
		sendError(sess, client, errIllegalOp, "illegal operation")
	}
}

// relName turns a requested filename into a clean root-relative path: leading
// slashes and any ".." components are stripped so it cannot be absolute or
// escape upward. rootFS additionally blocks symlink escapes at open time
func relName(name string) string {
	return strings.TrimPrefix(filepath.Clean("/"+name), string(os.PathSeparator))
}

// parseRequest extracts the filename and mode from an RRQ/WRQ packet body
// (everything after the 2-byte opcode)
func parseRequest(b []byte) (filename, mode string, err error) {
	parts := strings.Split(string(b), "\x00")
	if len(parts) < 2 || parts[0] == "" {
		return "", "", errors.New("malformed request")
	}
	return parts[0], strings.ToLower(parts[1]), nil
}

// serveRead handles an RRQ by streaming the file to the client blockSize bytes
// at a time, waiting for the matching ACK after each block
func serveRead(sess *net.UDPConn, client *net.UDPAddr, name string) {
	f, err := rootFS.Open(name)
	if err != nil {
		if os.IsNotExist(err) {
			sendError(sess, client, errFileNotFound, "file not found")
		} else {
			// Escape attempts and other errors: log detail, reveal nothing
			log.Println("open:", err)
			sendError(sess, client, errAccessViolation, "access violation")
		}
		return
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, blockSize)
	var block uint16 = 1
	for {
		n, err := io.ReadFull(f, buf)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			sendError(sess, client, errNotDefined, err.Error())
			return
		}
		// The first block gets a reduced retry budget to avoid amplifying
		// traffic toward spoofed sources; later blocks use the full budget.
		retries := maxRetry
		if block == 1 {
			retries = firstBlockRetries
		}
		if !sendAndWaitACK(sess, client, buildData(block, buf[:n]), block, retries) {
			return
		}
		// Any read shorter than a full block ends the transfer
		if n < blockSize {
			return
		}
		block++
	}
}

// serveWrite handles a WRQ by receiving DATA blocks and writing them to path,
// acknowledging each block. The file is removed if the transfer fails
func serveWrite(sess *net.UDPConn, client *net.UDPAddr, name string) {
	// O_EXCL makes "create only if absent" atomic (no TOCTOU) and, via rootFS,
	// refuses any path that escapes the served directory
	f, err := rootFS.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			sendError(sess, client, errFileExists, "file already exists")
		} else {
			log.Println("create:", err)
			sendError(sess, client, errAccessViolation, "access violation")
		}
		return
	}
	committed := false
	defer func() {
		_ = f.Close()
		if !committed {
			_ = rootFS.Remove(name)
		}
	}()

	// Send initial ACK for block 0 to start the transfer.
	var block uint16
	lastAck := buildACK(0)
	if _, err := sess.WriteToUDP(lastAck, client); err != nil {
		return
	}

	var written int64
	pkt := make([]byte, 4+blockSize)
	for {
		expected := block + 1
		n, err := readFromPeer(sess, client, pkt, lastAck)
		if err != nil {
			log.Println("write recv:", err)
			return
		}
		if n < 4 {
			continue
		}
		if op := binary.BigEndian.Uint16(pkt[:2]); op != opDATA {
			sendError(sess, client, errIllegalOp, "expected DATA")
			return
		}
		if got := binary.BigEndian.Uint16(pkt[2:4]); got != expected {
			// Duplicate or out-of-order: re-ACK the last good block.
			_, _ = sess.WriteToUDP(lastAck, client)
			continue
		}

		data := pkt[4:n]
		if maxWriteBytes > 0 && written+int64(len(data)) > maxWriteBytes {
			sendError(sess, client, errDiskFull, "file exceeds maximum size")
			return
		}
		if _, err := f.Write(data); err != nil {
			sendError(sess, client, errDiskFull, "disk full or write error")
			return
		}
		written += int64(len(data))

		block = expected
		lastAck = buildACK(block)
		if _, err := sess.WriteToUDP(lastAck, client); err != nil {
			return
		}
		if len(data) < blockSize {
			committed = true
			return
		}
	}
}

// buildData constructs a DATA packet for the given block and payload
func buildData(block uint16, data []byte) []byte {
	out := make([]byte, 4+len(data))
	binary.BigEndian.PutUint16(out[:2], opDATA)
	binary.BigEndian.PutUint16(out[2:4], block)
	copy(out[4:], data)
	return out
}

// buildACK constructs an ACK packet for the given block
func buildACK(block uint16) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint16(b[:2], opACK)
	binary.BigEndian.PutUint16(b[2:4], block)
	return b
}

// sendError sends an ERROR packet with the given code and message
func sendError(sess *net.UDPConn, to *net.UDPAddr, code uint16, msg string) {
	var buf bytes.Buffer
	// Writes to a bytes.Buffer never fail; the UDP send is best-effort.
	_ = binary.Write(&buf, binary.BigEndian, uint16(opERROR))
	_ = binary.Write(&buf, binary.BigEndian, code)
	buf.WriteString(msg)
	buf.WriteByte(0)
	_, _ = sess.WriteToUDP(buf.Bytes(), to)
}

// sendAndWaitACK sends a packet and waits for the matching ACK, retransmitting
// on timeout up to retries times. Packets from a wrong TID are rejected
func sendAndWaitACK(sess *net.UDPConn, client *net.UDPAddr, data []byte, block uint16, retries int) bool {
	ackBuf := make([]byte, 4)
	for try := 0; try < retries; try++ {
		if _, err := sess.WriteToUDP(data, client); err != nil {
			log.Println("send:", err)
			return false
		}
		_ = sess.SetReadDeadline(time.Now().Add(timeout))
		for {
			n, from, err := sess.ReadFromUDP(ackBuf)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					break // retransmit
				}
				log.Println("ack recv:", err)
				return false
			}
			if from.Port != client.Port || !from.IP.Equal(client.IP) {
				// Wrong TID — reply with error to that sender, keep waiting
				sendError(sess, from, errUnknownTID, "unknown transfer ID")
				continue
			}
			if n >= 4 && binary.BigEndian.Uint16(ackBuf[:2]) == opACK &&
				binary.BigEndian.Uint16(ackBuf[2:4]) == block {
				return true
			}
		}
	}
	log.Println("giving up after retries on block", block)
	return false
}

// readFromPeer reads one packet from the expected client, resending lastAck on
// each timeout (in case our ACK was lost) up to maxRetry times. Packets from a
// wrong TID are rejected without consuming a retry
func readFromPeer(sess *net.UDPConn, client *net.UDPAddr, buf, lastAck []byte) (int, error) {
	for try := 0; try < maxRetry; try++ {
		_ = sess.SetReadDeadline(time.Now().Add(timeout))
		n, from, err := sess.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				_, _ = sess.WriteToUDP(lastAck, client)
				continue
			}
			return 0, err
		}
		if from.Port != client.Port || !from.IP.Equal(client.IP) {
			sendError(sess, from, errUnknownTID, "unknown transfer ID")
			continue
		}
		return n, nil
	}
	return 0, errors.New("timeout")
}
