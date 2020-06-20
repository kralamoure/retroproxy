# d1sniff

`d1sniff` is a tool for sniffing packets from Dofus 1 login/game servers. It works by starting a proxy to a login
server and another one to a game server, while logging every packet between the connections, for further
analysis.

Some examples of what you can do with this tool:

- Reverse engineer a server
- Get map encryption keys
- Get map fight positions
- Get map trigger cells
- Get monster groups locations
- Get dialogs (questions and answers) from NPCs

## Requirements:

- [Git](https://git-scm.com/)
- [Go](https://golang.org/)

## Build

```sh
git clone https://github.com/kralamoure/d1sniff
cd d1sniff
go build
```

## Installation

```sh
go get github.com/kralamoure/d1sniff/cmd/d1sniff
```

## Usage

```sh
d1sniff --help
```
