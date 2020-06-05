# d1sniffer

`d1sniffer` is a tool for sniffing packets from a live Dofus 1 server. It works by starting a proxy to a Dofus 1 login
server for logging every packet between them, so you can analyze them later.

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
