# d1proxy

`d1proxy` is a proxy for Dofus 1 login and game servers.

## Requirements

- [Git](https://git-scm.com/)
- [Go](https://golang.org/)

## Build

```sh
git clone https://github.com/kralamoure/d1proxy
cd d1proxy
go build ./cmd/...
```

## Installation

```sh
go get -u -v github.com/kralamoure/d1proxy/...
```

## Usage

```sh
d1proxy --help

// Output
Usage of d1proxy:
  -v, --version         Print version
  -d, --debug           Enable debug mode
  -s, --server string   Dofus login server address (default "co-retro.ankama-games.com:443")
  -l, --login string    Dofus login proxy listener address (default "0.0.0.0:5555")
  -g, --game string     Dofus game proxy listener address (default "0.0.0.0:5556")
  -p, --public string   Dofus game proxy public address (default "127.0.0.1:5556")
  -a, --admin           Force admin mode on the client
```
