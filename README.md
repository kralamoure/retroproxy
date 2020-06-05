# d1sniffer

`d1sniffer` is a tool for sniffing packets from a live Dofus 1 server. It works by creating a proxy to a login
server, and eventually one to a game server, while logging every packet between the proxies and the servers, so you can
analyze the packets later.

If you start the proxy in production mode, it saves the logs in `proxy.log`. Also, it can automatically talk to every
NPC — while hiding from the client the dialogs caused by this behavior.

Some examples of what you can do with this tool:

- Reverse engineer a server
- Get map encryption keysW
- Get map fight positions
- Get map trigger cells
- Get monster groups locations
- Get dialogs (questions and answers) from NPCs

## Build

```sh
git clone https://github.com/kralamoure/d1sniffer
cd d1sniffer
go build ./cmd/...
```

### Requirements:

- [Git](https://git-scm.com/)
- [Go](https://golang.org/)

## Usage

```sh
proxy -help
```
