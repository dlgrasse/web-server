package main

import (
	"strings"
)

type HTTPMethod int

const (
	UNKNOWN HTTPMethod = iota
	GET
	PUT
	PATCH
	POST
	DELETE
	HEAD
	OPTIONS
	CONNECT
	TRACE
)

func AsMethod(methodStr string) (methodIota HTTPMethod, invalid httpError) {
	switch strings.ToUpper(methodStr) {
		case "GET": return GET, nil
		// case "PUT": return PUT, nil
		// case "PATCH": return PATCH, nil
		// case "POST": return POST, nil
		// case "DELETE": return DELETE, nil
		// case "HEAD": return HEAD, nil
		// case "OPTIONS": return OPTIONS, nil
		// case "CONNECT": return CONNECT, nil
		// case "TRACE": return TRACE, nil
		default: return UNKNOWN, newMethodNotAllowedError("invalid method '"+methodStr+"'")
	}
}

func (m HTTPMethod) String() string {
	switch m {
		case GET: return "GET"
		case PUT: return "PUT"
		case PATCH: return "PATCH"
		case POST: return "POST"
		case DELETE: return "DELETE"
		case HEAD: return "HEAD"
		case OPTIONS: return "OPTIONS"
		case CONNECT: return "CONNECT"
		case TRACE: return "TRACE"
		default: return "UNKNOWN"
	}
}
