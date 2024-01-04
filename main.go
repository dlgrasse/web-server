package main

import (
	"fmt"
	"os"
	"strings"
	"bytes"
	"io"
	"mime"
	"net"
	"strconv"
	"path/filepath"
)

func main() {
	config := getConfig()

	fmt.Printf("config=%v\n", config)

	listener, err := net.Listen("tcp", fmt.Sprint(":", config.port))
	if err != nil {
		fmt.Printf("Cannot listen to port %v\n", config.port)
		os.Exit(1)
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("error accepting connection on socket %v\n", err.Error())
			os.Exit(1)
		}
		go handleConnection(conn, config)
	}
}

func handleConnection(conn net.Conn, config configMap) {
	defer conn.Close()

	fmt.Printf("#handleConnection for %v\n", conn.RemoteAddr())

	for {
		startLine, slErr := parseStartLine(conn)
		if slErr != nil {
			processError(conn, slErr)
			break;
		}
		proxyProcessed, pErr := doProxy(conn, startLine, config)
		if pErr != nil {
			processError(conn, pErr)
			break;
		}
		if (!proxyProcessed) {
			headers, hErr := parseHeaders(conn)
			if hErr != nil {
				processError(conn, hErr)
				break;
			}
			_, bErr := parseBody(conn, headers)
			if bErr != nil {
				processError(conn, bErr)
				break;
			}

			rErr := doRequest(conn, startLine.resource, config)
			if rErr != nil {
				processError(conn, rErr)
				break;
			}

			fmt.Println("...request processed successfully")
		}
	}
}

type startLine struct {
	method HTTPMethod
	resource string
	version string
}
func (sl *startLine) String(prefix string) string {
	resource := sl.resource
	if prefix != "" {
		resource = resource[len(prefix):]
	}
	if resource == "" {
		resource = "/"
	}
	return sl.method.String()+" "+resource+" "+sl.version
}

func parseStartLine(conn net.Conn) (startLine, httpError) {
	fmt.Println("#parseStartLine")
	retVal := startLine{}

	charBuf := make([]byte, 1)
	buffer := bytes.NewBuffer(nil)
	part := 0

	for {
		_, readErr := conn.Read(charBuf)
		if readErr != nil {
			fmt.Printf("something happened reading from the start line: %v\n", readErr.Error())
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
				case 1: retVal.resource = buffer.String()
				case 2: retVal.version = buffer.String()
			}

			buffer.Reset()
			part++
		} else if nextChar == 10 {
			break
		} else {
			buffer.Write(charBuf[:])
		}
	}

	fmt.Printf("parsed out start-line: %v\n", retVal)
	return retVal, nil
}

func parseHeaders (conn net.Conn) (map[string]string, httpError) {
	fmt.Println("#parseHeaders")
	headerMap := make(map[string]string)

	endOfHeaders := false
	headerByte := make([]byte, 1)
	buffer := bytes.NewBuffer(nil)

	for {
		_, readErr := conn.Read(headerByte)
		if readErr != nil {
			fmt.Printf("something happened reading from the header section: %v\n", readErr.Error())
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

	for hKey,hVal := range headerMap {
		fmt.Printf("header '%v'=%v\n", hKey, hVal)
	}
	return headerMap, nil
}

func parseBody(conn net.Conn, headers map[string]string) (string, httpError) {
	fmt.Println("#parseBody")
	clHeader, clHeaderOK := headers["content-length"]
	if clHeaderOK {
		contentLength, clOk := strconv.Atoi(clHeader)
		if clOk == nil {
			fmt.Printf("'Content-Length'=%v\n", contentLength)
			bodyBytes := make([]byte, contentLength)

			_, bodyErr := conn.Read(bodyBytes) // TODO: maybe check if bodyLen doesn't match cl
			if bodyErr != nil {
				fmt.Printf("something happened reading the body content %v\n", bodyErr.Error())
				return "", newInternalServerErrorError(bodyErr.Error())
			}
			
			bodyContent := string(bodyBytes)
			fmt.Printf("body: %v\n", bodyContent)
			return bodyContent, nil
		}
		
		fmt.Printf("invalid 'Content-Length' header value: %v\n", clHeader)
		return "", newLengthRequiredError(clHeader)
	} else {
		return "", nil
	}
}

const (
	READ_REQ_LEN = 1024
	READ_PROXY_LEN = 2048
)
func doProxy(conn net.Conn, startLine startLine, config configMap) (wasProcessed bool, err httpError) {
	fmt.Println("#doProxy")
	resourceParts := strings.Split(startLine.resource, "/")
	proxyRoot := "/"+resourceParts[1]
	proxyPort, okpc := config.proxyContexts[proxyRoot]
	if okpc {
		fmt.Printf("...%v proxying to port %v\n", proxyRoot, proxyPort)
		proxyConn, pErr := net.Dial("tcp", fmt.Sprint("localhost:",proxyPort))
		if pErr != nil {
			return false, newInternalServerErrorError(pErr.Error())
		}
		
		fmt.Printf("...connected to proxy: %v\n", startLine.String(proxyRoot))
		proxyConn.Write([]byte(startLine.String(proxyRoot)))
		proxyConn.Write([]byte("\r\n"))

		reqBytes := make([]byte, READ_REQ_LEN) // TODO: is 1K a good size for forwarding the request?  maybe larger on the response?
		for {
			fmt.Println("...awaiting requestor bytes")
			readNum, readErr := conn.Read(reqBytes)
			if readErr != nil {
				if err != io.EOF {
					return false, newInternalServerErrorError(readErr.Error())
				} else {
					fmt.Println("...reached end of proxied request")
					break
				}
			}
			
			fmt.Printf("...writing %v bytes to proxy\n", string(reqBytes[0:readNum]))
			_, writeErr := proxyConn.Write(reqBytes[0:readNum])
			if writeErr != nil {
				return false, newInternalServerErrorError(writeErr.Error())
			}

			if readNum < READ_REQ_LEN {
				break;
			}
		}

		respBytes := make([]byte, READ_PROXY_LEN) // expect response to be larger, so 2k stream size?
		for {
			fmt.Println("...awaiting proxy response")
			readNum, readErr := proxyConn.Read(respBytes)
			if readErr != nil {
				if err != io.EOF {
					return false, newInternalServerErrorError(readErr.Error())
				} else {
					break
				}
			}
			
			fmt.Printf("...writing %v proxied bytes back to requestor\n", string(respBytes[0:readNum]))
			_, writeErr := conn.Write(respBytes[0:readNum])
			if writeErr != nil {
				return false, newInternalServerErrorError(writeErr.Error())
			}

			if readNum < READ_PROXY_LEN {
				break;
			}
		}

		return true, nil
	} else {
		fmt.Println("...no proxy configured")
		return false, nil
	}
}

func doRequest(conn net.Conn, resource string, config configMap) httpError {
	fmt.Printf("#doRequest for: %v\n", resource)

	if len(resource) == 0 { // is this even possible?  i suppose it could be
		resource = "/"
	}
	
	// determine if it's pointing to a virtual host or our root
	resourceParts := strings.Split(resource, "/")
	virtualHost := "/"+resourceParts[1]
	fmt.Printf("checking map %v for key %v\n", config.virtualHosts, virtualHost)
	virtualRoot, okvh := config.virtualHosts[virtualHost]
	if okvh {
		virtualPath := resource[len(virtualHost):]
		resource = fmt.Sprint(virtualRoot, virtualPath)
	} else {
		resource = fmt.Sprint(config.root, resource)
	}
	if string(resource[len(resource)-1]) == "/" { // TODO: maybe configure the 'index' file name?
		resource = resource+"index.html"
	}
	fmt.Printf("...actual resource %v\n", resource)

	fileInfo, statErr := os.Stat(resource)
	if statErr != nil {
		fmt.Printf("error stating file %v, %v\n", resource, statErr.Error())
		return newNotFoundError(statErr.Error())
	}
	
	fileSize := fileInfo.Size()
	fmt.Printf("...size %v\n", fileSize)

	fileBytes := make([]byte, fileSize)
	file, openErr := os.Open(resource)
	if openErr != nil {
		fmt.Printf("error opening file %v, %v\n", resource, openErr.Error())
		return newForbiddenError(statErr.Error())
	}
	
	_, readErr := file.Read(fileBytes)
	if readErr != nil {
		fmt.Printf("error reading from file %v, %v\n", resource, readErr.Error())
		return newForbiddenError(statErr.Error())
	}
	
	processResponse(conn, new200Response(), mimeForFile(resource), fileBytes)
	return nil
}

func processResponse(conn net.Conn, respStatus httpResponse, mimeType string, respBytes []byte) {
	fmt.Printf("responding with status/mime %v/%v\n", respStatus, mimeType)

	conn.Write([]byte(getStatusLine(respStatus)))
	conn.Write([]byte(fmt.Sprint("Content-Type: ", mimeType, "\r\n")))
	conn.Write([]byte(fmt.Sprint("Content-Length: ", len(respBytes), "\r\n")))
	conn.Write([]byte("\r\n"))
	conn.Write(respBytes)

}

func processError(conn net.Conn, err httpError) {
	fmt.Printf("responding with error %v\n", err)

	conn.Write([]byte(getStatusLine(err)))
	conn.Write([]byte("\r\n\r\n"))
}

func mimeForFile(fileName string) (mimeType string) {
	return mime.TypeByExtension(filepath.Ext(fileName))
}

func getStatusLine(resp httpResponse) string {
	return fmt.Sprint("HTTP/1.1 ", resp.Code(), " ", resp.Message(), "\r\n")
}
