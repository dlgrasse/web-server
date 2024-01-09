package main

import (
	"bytes"
	"fmt"
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
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	log.Info().Msgf("#handleConnection for %v", conn.RemoteAddr())

	for {
		startLine, slErr := parseStartLine(conn)
		if slErr != nil {
			processError(conn, slErr)
			break
		}
		headers, hErr := parseHeaders(conn)
		if hErr != nil {
			processError(conn, hErr)
			break
		}

		proxyProcessed, pErr := doProxy(conn, startLine, headers)
		if pErr != nil {
			processError(conn, pErr)
			break
		}
		if !proxyProcessed {
			_, bErr := parseBody(conn, headers)
			if bErr != nil {
				processError(conn, bErr)
				break
			}

			rErr := doRequest(conn, startLine.resource)
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
			buffer.Write(charBuf)
		}
	}

	log.Debug().Msgf("parsed out start-line: %v", retVal)
	return retVal, nil
}

func parseBody(conn net.Conn, headers headers) ([]byte, httpError) {
	log.Trace().Msg("#parseBody")
	clHeader, clHeaderOK := headers.Value("Content-Length")
	if clHeaderOK {
		contentLength, clOk := strconv.Atoi(clHeader)
		if clOk == nil {
			log.Trace().Msgf("'Content-Length'=%v", contentLength)
			bodyBytes := make([]byte, contentLength)

			_, bodyErr := conn.Read(bodyBytes) // TODO: maybe check if bodyLen doesn't match cl
			if bodyErr != nil {
				log.Error().Err(bodyErr).Msg("something happened reading the body content")
				return nil, newInternalServerErrorError(bodyErr.Error())
			}

			bodyContent := bodyBytes
			// log.Trace().Msgf("body: %v", bodyContent)
			return bodyContent, nil
		}

		log.Info().Msgf("invalid 'Content-Length' header value: %v", clHeader)
		return nil, newLengthRequiredError(clHeader)
	} else {
		return nil, nil
	}
}

func doRequest(conn net.Conn, resource string) httpError {
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
