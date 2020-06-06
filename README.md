# d1sniff

`d1sniff` is a tool for sniffing packets from a Dofus 1 server. It works by starting a proxy to a login
server, and eventually one to a game server, while logging every packet between the proxies and the servers, so you can
analyze the packets later.

If you start it in production mode, it appends the logs to `d1sniff.log`. Also, it can automatically talk to every
NPC — while hiding from the client the dialogs caused by this behavior.

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
go build ./cmd/d1sniff
```

## Installation

```sh
go get github.com/kralamoure/d1sniff/cmd/d1sniff
```

## Usage

```sh
d1sniff -help
```
