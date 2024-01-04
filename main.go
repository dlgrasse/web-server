package main

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
    "github.com/rs/zerolog/log"
)

func main() {
	config := getConfig()

	listener, err := net.Listen("tcp", fmt.Sprint(":", config.port))
	if err != nil {
		log.Fatal().Err(err).Msgf("Cannot listen to port %v", config.port)
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatal().Err(err).Msg("error accepting connection on socket")
		}
		go handleConnection(conn, config)
	}
}

func handleConnection(conn net.Conn, config configMap) {
	defer conn.Close()

	log.Info().Msgf("#handleConnection for %v", conn.RemoteAddr())

	for {
		startLine, slErr := parseStartLine(conn)
		if slErr != nil {
			processError(conn, slErr)
			break
		}
		proxyProcessed, pErr := doProxy(conn, startLine, config)
		if pErr != nil {
			processError(conn, pErr)
			break
		}
		if !proxyProcessed {
			headers, hErr := parseHeaders(conn)
			if hErr != nil {
				processError(conn, hErr)
				break
			}
			_, bErr := parseBody(conn, headers)
			if bErr != nil {
				processError(conn, bErr)
				break
			}

			rErr := doRequest(conn, startLine.resource, config)
			if rErr != nil {
				processError(conn, rErr)
				break
			}

			log.Debug().Msg("...request processed successfully")
		}
	}
}

type startLine struct {
	method   HTTPMethod
	resource string
	version  string
}

func (sl *startLine) String(prefix string) string {
	resource := sl.resource
	if prefix != "" {
		resource = resource[len(prefix):]
	}
	if resource == "" {
		resource = "/"
	}
	return sl.method.String() + " " + resource + " " + sl.version
}

func parseStartLine(conn net.Conn) (startLine, httpError) {
	log.Trace().Msg("#parseStartLine")
	retVal := startLine{}

	charBuf := make([]byte, 1)
	buffer := bytes.NewBuffer(nil)
	part := 0

	for {
		_, readErr := conn.Read(charBuf)
		if readErr != nil {
			log.Error().Err(readErr).Msg("something happened reading from the start line")
			return retVal, newInternalServerErrorError(readErr.Error())
		}

		nextChar := charBuf[0]
		if nextChar == 32 || nextChar == 13 {
			switch part {
			case 0:
				httpMethod, metErr := AsMethod(buffer.String())
				if metErr != nil {
					return retVal, metErr
				}
				retVal.method = httpMethod
			case 1:
				retVal.resource = buffer.String()
			case 2:
				retVal.version = buffer.String()
			}

			buffer.Reset()
			part++
		} else if nextChar == 10 {
			break
		} else {
			buffer.Write(charBuf[:])
		}
	}

	log.Printf("parsed out start-line: %v", retVal)
	return retVal, nil
}

func parseHeaders(conn net.Conn) (map[string]string, httpError) {
	log.Trace().Msg("#parseHeaders")
	headerMap := make(map[string]string)

	endOfHeaders := false
	headerByte := make([]byte, 1)
	buffer := bytes.NewBuffer(nil)

	for {
		_, readErr := conn.Read(headerByte)
		if readErr != nil {
			log.Error().Err(readErr).Msg("something happened reading from the header section")
			return headerMap, newInternalServerErrorError(readErr.Error())
		}

		nextChar := headerByte[0]
		if nextChar == 10 { // end of headers after 2nd CRLF
			if endOfHeaders {
				break
			} else {
				endOfHeaders = true
			}
		} else if nextChar == 13 { // at the end of the line (CR), parse out content and add to our header map
			if !endOfHeaders { // this will be true on 2nd consecutive CRLF
				headerRow := buffer.String()
				headerSeparatorIdx := strings.Index(headerRow, ":")
				if headerSeparatorIdx > 0 { // this will happen on 2nd CRLF
					headerName := strings.ToLower(headerRow[0:headerSeparatorIdx]) // normalizing on lower-case in case header names are sent w/o case regard
					headerValue := headerRow[headerSeparatorIdx+1:]

					headerMap[headerName] = headerValue
				}

				buffer.Reset()
			}
		} else {
			endOfHeaders = false // if we got a non-CRLF, we're still processing header content
			buffer.Write(headerByte[0:1])
		}
	}

	for hKey, hVal := range headerMap {
		log.Trace().Msgf("header '%v'=%v", hKey, hVal)
	}
	return headerMap, nil
}

func parseBody(conn net.Conn, headers map[string]string) (string, httpError) {
	log.Trace().Msg("#parseBody")
	clHeader, clHeaderOK := headers["content-length"]
	if clHeaderOK {
		contentLength, clOk := strconv.Atoi(clHeader)
		if clOk == nil {
			log.Trace().Msgf("'Content-Length'=%v", contentLength)
			bodyBytes := make([]byte, contentLength)

			_, bodyErr := conn.Read(bodyBytes) // TODO: maybe check if bodyLen doesn't match cl
			if bodyErr != nil {
				log.Error().Err(bodyErr).Msg("something happened reading the body content")
				return "", newInternalServerErrorError(bodyErr.Error())
			}

			bodyContent := string(bodyBytes)
			log.Printf("body: %v", bodyContent)
			return bodyContent, nil
		}

		log.Info().Msgf("invalid 'Content-Length' header value: %v", clHeader)
		return "", newLengthRequiredError(clHeader)
	} else {
		return "", nil
	}
}

const (
	READ_REQ_LEN   = 1024
	READ_PROXY_LEN = 2048
)

func doProxy(conn net.Conn, startLine startLine, config configMap) (wasProcessed bool, err httpError) {
	log.Trace().Msg("#doProxy")
	resourceParts := strings.Split(startLine.resource, "/")
	proxyRoot := "/" + resourceParts[1]
	proxyPort, okpc := config.proxyContexts[proxyRoot]
	if okpc {
		log.Debug().Msgf("...%v proxying to port %v", proxyRoot, proxyPort)
		proxyConn, pErr := net.Dial("tcp", fmt.Sprint("localhost:", proxyPort))
		if pErr != nil {
			return false, newInternalServerErrorError(pErr.Error())
		}

		log.Printf("...connected to proxy: %v", startLine.String(proxyRoot))
		proxyConn.Write([]byte(startLine.String(proxyRoot)))
		proxyConn.Write([]byte("\r\n"))

		reqBytes := make([]byte, READ_REQ_LEN) // TODO: is 1K a good size for forwarding the request?  maybe larger on the response?
		for {
			log.Trace().Msg("...awaiting requestor bytes")
			readNum, readErr := conn.Read(reqBytes)
			if readErr != nil {
				if err != io.EOF {
					return false, newInternalServerErrorError(readErr.Error())
				} else {
					log.Trace().Msg("...reached end of proxied request")
					break
				}
			}

			log.Trace().Msgf("...writing %v bytes to proxy", string(reqBytes[0:readNum]))
			_, writeErr := proxyConn.Write(reqBytes[0:readNum])
			if writeErr != nil {
				return false, newInternalServerErrorError(writeErr.Error())
			}

			if readNum < READ_REQ_LEN {
				break
			}
		}

		respBytes := make([]byte, READ_PROXY_LEN) // expect response to be larger, so 2k stream size?
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

			log.Trace().Msgf("...writing %v proxied bytes back to requestor", string(respBytes[0:readNum]))
			_, writeErr := conn.Write(respBytes[0:readNum])
			if writeErr != nil {
				return false, newInternalServerErrorError(writeErr.Error())
			}

			if readNum < READ_PROXY_LEN {
				break
			}
		}

		return true, nil
	} else {
		log.Trace().Msg("...no proxy configured")
		return false, nil
	}
}

func doRequest(conn net.Conn, resource string, config configMap) httpError {
	log.Debug().Msgf("#doRequest for: %v", resource)

	if len(resource) == 0 { // is this even possible?  i suppose it could be
		resource = "/"
	}

	// determine if it's pointing to a virtual host or our root
	resourceParts := strings.Split(resource, "/")
	virtualHost := "/" + resourceParts[1]
	log.Trace().Msgf("checking map %v for key %v", config.virtualHosts, virtualHost)
	virtualRoot, okvh := config.virtualHosts[virtualHost]
	if okvh {
		virtualPath := resource[len(virtualHost):]
		resource = fmt.Sprint(virtualRoot, virtualPath)
	} else {
		resource = fmt.Sprint(config.root, resource)
	}
	if string(resource[len(resource)-1]) == "/" { // TODO: maybe configure the 'index' file name?
		resource = resource + "index.html"
	}
	log.Trace().Msgf("...actual resource %v", resource)

	fileInfo, statErr := os.Stat(resource)
	if statErr != nil {
		log.Error().Err(statErr).Msgf("error stating file %v", resource)
		return newNotFoundError(statErr.Error())
	}

	fileSize := fileInfo.Size()
	log.Printf("...size %v", fileSize)

	fileBytes := make([]byte, fileSize)
	file, openErr := os.Open(resource)
	if openErr != nil {
		log.Error().Err(openErr).Msgf("error opening file %v", resource)
		return newForbiddenError(statErr.Error())
	}

	_, readErr := file.Read(fileBytes)
	if readErr != nil {
		log.Error().Err(readErr).Msgf("error reading file %v", resource)
		return newForbiddenError(statErr.Error())
	}

	processResponse(conn, new200Response(), mimeForFile(resource), fileBytes)
	return nil
}

func processResponse(conn net.Conn, respStatus httpResponse, mimeType string, respBytes []byte) {
	log.Trace().Msgf("responding with status/mime %v/%v", respStatus, mimeType)

	conn.Write([]byte(getStatusLine(respStatus)))
	conn.Write([]byte(fmt.Sprint("Content-Type: ", mimeType, "\r\n")))
	conn.Write([]byte(fmt.Sprint("Content-Length: ", len(respBytes), "\r\n")))
	conn.Write([]byte("\r\n"))
	conn.Write(respBytes)

}

func processError(conn net.Conn, err httpError) {
	log.Trace().Err(err).Msgf("responding with error")

	conn.Write([]byte(getStatusLine(err)))
	conn.Write([]byte("\r\n\r\n"))
}

func mimeForFile(fileName string) (mimeType string) {
	return mime.TypeByExtension(filepath.Ext(fileName))
}

func getStatusLine(resp httpResponse) string {
	return fmt.Sprint("HTTP/1.1 ", resp.Code(), " ", resp.Message(), "\r\n")
}
