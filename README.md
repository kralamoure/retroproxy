# d1proxy

`d1proxy` is a tool for sniffing packets from Dofus 1 login/game servers. It works by starting a proxy to a login
server and another one to a game server, while logging every packet between the connections, for further
analysis.

Some examples of what you can do with this tool:

- Reverse engineer a server
- Get map encryption keys
- Get map fight positions
- Get map trigger cells
- Get monster groups locations
- Get dialogs (questions and answers) from NPCs

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
```
