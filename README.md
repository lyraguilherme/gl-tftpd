# GL-TFTPD

[![CI](https://github.com/lyraguilherme/gl-tftpd/actions/workflows/ci.yml/badge.svg)](https://github.com/lyraguilherme/gl-tftpd/actions/workflows/ci.yml)

A small, dependency-free [TFTP](https://datatracker.ietf.org/doc/html/rfc1350)
server written in Go. Serves files from a directory over UDP, with optional
uploads. Reads are on by default; writes are opt-in.

## Features

- **RFC 1350 TFTP** in `octet` (binary) mode — RRQ (download) and WRQ (upload).
- **Sandboxed root.** All file access goes through `os.Root`, so requests cannot
  escape the served directory via `..` or symlinks.
- **Writes off by default.** Enable uploads explicitly with `-writable`.
- **Upload size cap** (`-max-write-bytes`) to limit disk usage.
- **Per-session transfer IDs**, retransmission on timeout, and rejection of
  packets from an unexpected source (TID) — the basic TFTP correctness bits.
- No third-party dependencies; a single static binary.

## Install

```sh
go install github.com/lyraguilherme/gl-tftpd@latest
```

Or build from source:

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

## Security notes

TFTP has **no authentication or encryption** — every file readable under `-root`
is public to anyone who can reach the port, and traffic is plaintext. Run it only
on trusted networks, bind it to a specific interface, and keep `-writable` off
unless you need uploads. The server sandboxes file access to `-root`, but it does
not protect against exposure of whatever you place in that directory.

## Development

```sh
go vet ./...
go test -race ./...
go build .
```

## License

No license yet — add one before publishing if you want others to reuse the code.
