# Web-Server
## Concept
The idea behind this repository is simply to learn GoLang.  If anybody actually looks at this and has any feedback, then hey - send it on.
I figured a web-server would be a good starting point as the following concepts are involved:
* building an executable
* threading
* streams
* string parsing
* importing libraries (i plan to use at least the following)
  - command-line parsing/switches
  - yaml parser (maybe even .properties to allow user preference?)
* properties-file usage

## Features
Who knows that the final product will look like, but I expect to follow an Apache-style application.
That is, a pure web-server, as opposed to an application server (like Tomcat or NodeJS).
It will have an entry point from which to stream back static content.
It will also support a configurable proxy file (a la mod_proxy) where backing servers of anytype can be forwarded requests to and their responses returned.

## Process
1. ./web-server [-port|--p (8080)] [-config|--c (./.config.yml)] </path/to/root>
   * return error if invalid port is specified
   * return error if invalid config file is specified
2. Listens on '--port'
3. Whenever an input stream is read, spawn a new thread to process the request
  * return 403 if path traverses above root
    - GET ../
    - GET /..
    - GET /folder/../..
  * return 404 if path doesn't exist
4. read all file-system files as streams

## Proxy
The config file will be of structure
`proxy:
  path: /ServiceContext
  port: <port of backing service>
    - <port if multiple>
    - <port if multiple>
  timeout: [100ms]
  maxAttemtps: [3]
`

The idea here being that if the path given in the HTTP request matches any of these contexts, then the request is forwarded to the service listening on that port.
If multiple ports are given, then it is assumed to need load-balancing, and a simple round-robin strategy is taken.
If 'timeout' is exceeded to connect, then that port is skipped and on to the next.
If 'maxAttempts' through all ports (meaning 'maxAttempts' * <num ports>) fails, then a  503 error is returned
