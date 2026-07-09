# GL-TFTPD

[![CI](https://github.com/lyraguilherme/gl-tftpd/actions/workflows/ci.yml/badge.svg)](https://github.com/lyraguilherme/gl-tftpd/actions/workflows/ci.yml)
[![CodeQL](https://github.com/lyraguilherme/gl-tftpd/actions/workflows/codeql.yml/badge.svg)](https://github.com/lyraguilherme/gl-tftpd/actions/workflows/codeql.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/lyraguilherme/gl-tftpd.svg)](https://pkg.go.dev/github.com/lyraguilherme/gl-tftpd)
[![License: MIT](https://img.shields.io/github/license/lyraguilherme/gl-tftpd)](LICENSE)

A small, dependency-free [TFTP](https://datatracker.ietf.org/doc/html/rfc1350)
server written in Go. Serves files from a directory over UDP, with optional
uploads. Reads are on by default; writes are opt-in.

## Features

- **RFC 1350 TFTP** in `octet` (binary) mode. RRQ (download) and WRQ (upload).
- **Writes off by default.** Enable uploads explicitly with `-writable` parameter.
- **Per-session transfer IDs**, retransmission on timeout, and rejection of
  packets from an unexpected source (TID) — the basic TFTP correctness bits.
- **Upload size cap** (`-max-write-bytes`) to limit disk usage.
- No third-party dependencies. No installation needed. Single static binary.

## Usage

### Download a prebuilt binary (recommended)

Grab the build for your OS/arch from the
[latest release](https://github.com/lyraguilherme/gl-tftpd/releases/latest) —
it's a single self-contained executable, no runtime needed.

```sh
# Linux x86-64 (adjust the suffix for your platform)
curl -Lo gl-tftpd https://github.com/lyraguilherme/gl-tftpd/releases/latest/download/gl-tftpd-linux-amd64
chmod +x gl-tftpd
./gl-tftpd -root /srv/tftp
```

Available builds: `linux-amd64`, `linux-arm64`, `darwin-amd64`, `darwin-arm64`,
`windows-amd64.exe`.

### Build it yourself

If you have the Go toolchain:

```sh
go install github.com/lyraguilherme/gl-tftpd@latest   # installs to $GOBIN
```

or from a checkout:

```sh
git clone https://github.com/lyraguilherme/gl-tftpd
cd gl-tftpd
go build .        # produces ./gl-tftpd
```

## Usage

Serve the current directory read-only on the default TFTP port (69):

```sh
sudo ./gl-tftpd -root /srv/tftp
```

> Port 69 is privileged, so binding it needs root (or a capability). Use a high
> port like `-addr :6969` to run unprivileged during development.

Allow uploads, capped at 50 MiB, on a high port:

```sh
./gl-tftpd -addr :6969 -root /srv/tftp -writable -max-write-bytes 52428800
```

Fetch and send with a standard client:

```sh
tftp 127.0.0.1 6969 -c get file.bin
tftp 127.0.0.1 6969 -c put local.bin uploaded.bin   # requires -writable
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `:69` | Listen address (`host:port`). |
| `-root` | `.` | Directory to serve. |
| `-writable` | `false` | Allow clients to upload files (WRQ). |
| `-max-write-bytes` | `1073741824` | Max bytes accepted per upload (0 = unlimited). |
| `-max-sessions` | `256` | Max concurrent transfers; excess requests are dropped. |

## Security notes

TFTP has **no authentication or encryption** — every file readable under `-root`
is public to anyone who can reach the port, and traffic is plaintext. Run it only
on trusted networks, bind it to a specific interface, and keep `-writable` off
unless you need uploads. The server sandboxes file access to `-root`, but it does
not protect against exposure of whatever you place in that directory.

### Known limitations

- **UDP reflection/amplification.** Because requests are unauthenticated UDP, a
  spoofed source address can make the server send data to a third party. This is
  mitigated — the first data block is sent only once, so the server won't
  repeatedly amplify traffic toward a forged source — but not eliminated (any
  single reflected block is inherent to stateless UDP). Don't expose it directly
  to the public internet.
- **Concurrency is capped, not rate-limited.** Concurrent transfers are bounded
  by `-max-sessions` (default 256); excess requests are dropped so a flood can't
  exhaust goroutines or file descriptors. There is no per-source rate limiting,
  so a single host can still churn through that budget — front it with a firewall
  on untrusted networks.
- **`octet` mode only.** `netascii` and `mail` transfer modes are not supported.
- **No RFC 2347 option negotiation** (`blksize`, `tsize`, `timeout`), so
  transfers always use 512-byte blocks.

## Development

```sh
go vet ./...
go test -race ./...
go build .
```

## License

[MIT](LICENSE) © 2026 Guilherme Lyra
