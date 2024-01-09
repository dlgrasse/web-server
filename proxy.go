package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"github.com/rs/zerolog/log"
	"io"
	"net"
	"strings"
)

const (
	READ_REQ_LEN   = 1024
	READ_PROXY_LEN = 2048
	LD_COOKIE      = "LDBLNCNGCK"
	LD_COOKIE_LEN  = 20
)

func doProxy(conn net.Conn, startLine startLine, headers headers) (wasProcessed bool, err httpError) {
	log.Trace().Msg("#doProxy")
	resourceParts := strings.Split(startLine.resource, "/")
	proxyRoot := "/" + resourceParts[1]
	proxyPorts, okpc := config.proxyContexts[proxyRoot]
	if okpc {
		_, alreadyConnected := headers.Cookie(LD_COOKIE)
		proxyConn, lbCookie, replaceCookie, proxyErr := forwardTo(proxyRoot, proxyPorts, headers)
		if proxyErr != nil {
			return false, proxyErr
		}

		buffer := bytes.NewBuffer(nil)

		proxyConn.Write([]byte(startLine.String(proxyRoot)))
		buffer.Write([]byte(startLine.String(proxyRoot)))
		proxyConn.Write([]byte("\r\n"))
		buffer.Write([]byte("\r\n"))

		// don't forward my load-balancing cookie to the proxied server
		headers.DeleteCookie(LD_COOKIE)
		proxyConn.Write([]byte(headers.String()))
		buffer.Write([]byte(headers.String()))
		proxyConn.Write([]byte("\r\n"))
		buffer.Write([]byte("\r\n"))

		bodyBytes, bodyErr := parseBody(conn, headers)
		if bodyErr != nil {
			return false, bodyErr
		}
		log.Trace().Msgf("writing body of %v length", len(bodyBytes))
		proxyConn.Write(bodyBytes)
		buffer.Write(bodyBytes)

		log.Trace().Msgf("wrote to proxied server: %v", buffer.String())

		// i need to send the first line, then parse out headers, add in my 'LD_COOKIE' one, then send them through to the client, then send the rest...phew!
		respBytes := make([]byte, 1) // expect response to be larger, so 2k stream size?
		for {
			log.Trace().Msg("...awaiting proxy response")
			readNum, readErr := proxyConn.Read(respBytes)
			if readErr != nil {
				if err != io.EOF {
					return false, newInternalServerErrorError(readErr.Error())
				} else {
					break
				}
			}
			_, writeErr := conn.Write(respBytes[0:readNum])
			if writeErr != nil {
				return false, newInternalServerErrorError(writeErr.Error())
			}

			// check if we're done with the first line
			if respBytes[0] == 10 {
				break
			}
		}
		headers, hErr := parseHeaders(proxyConn)
		if hErr != nil {
			return false, hErr
		}

		if replaceCookie {
			headers.ExpireCookie(LD_COOKIE)
		}
		if !alreadyConnected {
			headers.SetCookie(LD_COOKIE, lbCookie)
		}
		log.Trace().Msgf("response headers: %v", headers.String())
		conn.Write([]byte(headers.String()))
		conn.Write([]byte("\r\n"))

		respBytes = make([]byte, READ_PROXY_LEN) // expect response to be larger, so 2k stream size?
		for {
			log.Trace().Msg("...awaiting proxy response")
			readNum, readErr := proxyConn.Read(respBytes)
			if readErr != nil {
				if err != io.EOF {
					return false, newInternalServerErrorError(readErr.Error())
				} else {
					break
				}
			}

			// log.Trace().Msgf("...writing %v proxied bytes back to requestor", string(respBytes[0:readNum]))
			_, writeErr := conn.Write(respBytes[0:readNum])
			log.Trace().Msgf("responded with %v bytes to %v", readNum, startLine.String(proxyRoot))
			if writeErr != nil {
				return false, newInternalServerErrorError(writeErr.Error())
			}

			if readNum < READ_PROXY_LEN {
				break
			}
		}
		log.Debug().Msgf("completed successful response for %v", startLine.String(proxyRoot))

		return true, nil
	} else {
		log.Trace().Msgf("...no proxy configured for %v", proxyRoot)
		return false, nil
	}
}

// maps cookies to proxy-ports
var LD_PROXY_IDX_MAP = make(map[string]int)
var LD_COOKIE_PORT_MAP = make(map[string]int)

func forwardTo(proxyRoot string, proxyPorts []int, headers headers) (proxyConn net.Conn, lbCookie string, replace bool, err httpError) {
	replaceCookie := false
	ldCookie, ldExists := headers.Cookie(LD_COOKIE)
	if !ldExists {
		log.Trace().Msgf("no cookie for %v, creating a new one", proxyRoot)
		ldCookie = newCookie()
	} else {
		_, cookieHasPort := LD_COOKIE_PORT_MAP[ldCookie]
		replaceCookie = !cookieHasPort
	}
	log.Debug().Msgf("forwarding based on cookie: %v", ldCookie)

	// use the same port as last time, based on cookie
	proxyPort, connectionExists := LD_COOKIE_PORT_MAP[ldCookie]
	if !connectionExists {
		log.Trace().Msgf("no connection yet for %v", proxyRoot)
		// if no cookie (meaning this is the first time this client has connected)
		//	get the next port for this proxy (round-robin)
		proxyIdx, proxyConnected := LD_PROXY_IDX_MAP[proxyRoot]
		// if this is the first time ever for this proxied server, start before the beginning
		if !proxyConnected {
			log.Trace().Msgf("first time %v has connected", proxyRoot)
			proxyIdx = -1
		}
		// inc the load-balancing/round-robin index (mod for the number of ports configured)
		proxyIdx = (proxyIdx + 1) % len(proxyPorts)
		log.Trace().Msgf("...so %v to port index %v", proxyRoot, proxyIdx)
		LD_PROXY_IDX_MAP[proxyRoot] = proxyIdx
		// and finally set our cookie-to-port mapping
		proxyPort = proxyPorts[proxyIdx]
		LD_COOKIE_PORT_MAP[ldCookie] = proxyPort
	}

	log.Debug().Msgf("...%v proxying to port %v", proxyRoot, proxyPort)
	proxyConn, pErr := net.Dial("tcp", fmt.Sprint("127.0.0.1:", proxyPort))
	if pErr != nil {
		return nil, "", false, newInternalServerErrorError(pErr.Error())
	}

	return proxyConn, ldCookie, replaceCookie, nil
}

func newCookie() string {
	randBytes := make([]byte, LD_COOKIE_LEN+2)
	rand.Read(randBytes)
	cookieVal := base64.RawURLEncoding.EncodeToString(randBytes)
	return cookieVal[2 : LD_COOKIE_LEN+2]
}
