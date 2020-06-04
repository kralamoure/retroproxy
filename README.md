# d1scraper

`d1scraper` is a tool for scraping data from a live Dofus 1 server. It works by starting a proxy to a Dofus 1
login server for logging every packet between them, so they can be analyzed later.

The proxy saves the logs in `logs/proxy.log`. It automatically talks to every NPC.

## Usage

### Starting the proxy

```console
$ proxy -help 
```
