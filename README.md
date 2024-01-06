# Web-Server
## Concept
The idea behind this repository is simply to learn GoLang.  If anybody actually looks at this and has any feedback, then hey - send it on.
I figured a web-server would be a good starting point as the following concepts are involved:
* building an executable
* threading
* streams
* string parsing
* importing libraries (i plan to use at least the following)

## Features
Who knows what the final product will look like, but I expect to follow an Apache-style application.
That is, a pure web-server, as opposed to an application server (like Tomcat or NodeJS).
It will have an entry point from which to stream back static content.
It will also support a configurable proxy file (a la mod_proxy) where backing servers can be forwarded requests to and their responses returned.

## Usage
./web-server [-port (8080)] [-root (./root)] [-config (./.config.properties)] [-logLevel (info)]

## Configuration
In all cases command-line args > config-file properties > default values.  See 'Usage' above for command-line options

## Release Notes
### tag v0.0.1
- Initial tag
- command-line, .properties, and default configuration support
- 'root' directory
- any number of 'virtual root' directories
- 'localhost' proxy support against single port per proxied
- 'zerolog' logging

## Roadmap
- .yml configuration alternative
- Load balancing to proxied servers
  - simple round-robin
  - affinity with use of cookies
  - if no cookie (first-time requests) then cycle through ports after timeout
- Timeout for connecting to proxied servers
  - add to config file
- Orchestration support
  - setup control port
  - reset log level
  - reconfigure against config file
- what else 'Golang'-ish can i add?
  - channels (probably with 'Orchestration' feature)
- 
