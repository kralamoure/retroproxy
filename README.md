# d1scraper

`d1scraper` is a tool for scraping data from a live Dofus 1 server. It works by starting a proxy to a Dofus 1 login
server for logging every packet between them, so you can analyze them later.

If you start the proxy in production mode, it saves the logs in `proxy.log`. Also, it can automatically talk to every
NPC — while hiding from the client the dialogs caused by this behavior.

Some examples of what you can do with this tool:

- Reverse engineer the server
- Get map encryption keys
- Get map fight positions
- Get map trigger cells
- Get dialogs from NPCs


## Usage

### Starting the proxy

```console
$ proxy -help 
```
