package main

import (
	"fmt"
	"os"
	"strings"
	"bytes"
	"flag"
	"mime"
	"net"
	"path/filepath"
	"github.com/magiconair/properties"
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

	for {
		errBreak := false
		part := 0
		eof := 0
		firstLine := true
		methodBuf := bytes.NewBuffer(nil)
		resourceBuf := bytes.NewBuffer(nil)
		versionBuf := bytes.NewBuffer(nil)
		headerBuf := bytes.NewBuffer(nil)
		headers := make(map[string]string)
		charBuf := make([]byte, 1)
		for { // we're just processing the first line; otherwise end after 2 consecutive CRLF or end of #Read
			n, err := conn.Read(charBuf)
			if err != nil {
				fmt.Printf("something happened reading from the connection %v\n", err.Error())
				errBreak = true
				break
			} else if n == 1 { // check char if 1 is read
				if firstLine { // if we're still on the first line, parse out method/resource/version
					if charBuf[0] == 13 { // CR which means we're at the end of the first line so ignore
						continue
					} else if charBuf[0] == 10 { // LF so bump the eof  and skip this first-line section
						firstLine = false;
						eof++
					} else if charBuf[0] == 32 { // skip the char if a space or a CR (which bumps us off the first line)
						part++
					} else {
						switch part {
							case 0:
								methodBuf.Write(charBuf[0:1])
							case 1:
								resourceBuf.Write(charBuf[0:1])
							default:
								versionBuf.Write(charBuf[0:1])
						}
					}
				} else { // put all this stuff into the header buffer and exhause the request content
					if charBuf[0] == 10 { // inc 'eof' after CRLF
						eof++
						if eof == 2 { // stop after 2 consecutive CRLF
							break
						}
					} else if charBuf[0] == 13 { // at the end of the line, parse out content and add to our header map
						headerSeparatorIdx := strings.Index(headerBuf.String(), ":")
						if headerSeparatorIdx > 0 { // this will happen on 2nd CRLF
							headerName := strings.ToLower(headerBuf.String()[0:headerSeparatorIdx])
							headerValue := strings.TrimSpace(headerBuf.String()[headerSeparatorIdx+1:])
							headers[headerName] = headerValue
						}

						headerBuf.Reset()
					} else { // reset our eof counter if we read a non-CRLF char and add to our header buffer
						eof = 0
						headerBuf.Write(charBuf[0:1])
					}	
				}
			} else {
				fmt.Println("read 0 bytes from net.Conn")
				break
			}
		}
		if errBreak {
			break
		}
		fmt.Printf("method '%v' resource '%v' version '%v'\n", methodBuf.String(), resourceBuf.String(), versionBuf.String())
		for hKey,hVal := range headers {
			fmt.Printf("...with header '%v'=%v\n", hKey, hVal)
		}
		contentLength, clOk := headers["content-length"]
		if clOk {
			fmt.Printf("content-length=%v\n", contentLength)
		}
	
		var statusLine string
	
		validMethod := checkMethod(methodBuf.String())
		if !validMethod {
			statusLine = getStatusLine(405)
		}
	
		resourceStr := resourceBuf.String()
		if len(resourceStr) == 0 {
			resourceStr = "/"
		}
	
		// determine if it's poiting to a virtual host or our root
		var requestedResource string
		resourceParts := strings.Split(resourceStr, "/")
		virtualHost := "/"+resourceParts[1]
		fmt.Printf("checking map %v for key %v\n", config.virtualHosts, virtualHost)
		virtualRoot, okvh := config.virtualHosts[virtualHost]
		if okvh {
			virtualPath := resourceStr[len(virtualHost):]
			requestedResource = fmt.Sprint(virtualRoot, virtualPath)
		} else {
			requestedResource = fmt.Sprint(config.root, resourceStr)
		}
		if string(requestedResource[len(requestedResource)-1]) == "/" {
			requestedResource = requestedResource+"index.html"
		}
		var fileSize int64
		var fileBytes []byte
		okResp := false
		fmt.Printf("reading resource %v...\n", requestedResource)
	
		fileInfo, statErr := os.Stat(requestedResource)
		if statErr != nil {
			fmt.Printf("error stating file %v, %v\n", requestedResource, statErr.Error())
			statusLine = getStatusLine(404)
		} else {
			fileSize = fileInfo.Size()
			fmt.Printf("...size %v...\n", fileSize)
		
			fileBytes = make([]byte, fileSize)
			file, openErr := os.Open(requestedResource)
			if openErr != nil {
				fmt.Printf("error opening file %v, %v\n", requestedResource, openErr.Error())
				statusLine = getStatusLine(403)
			} else {
				numRead, readErr := file.Read(fileBytes)
				if readErr != nil {
					fmt.Printf("error reading from file %v, %v\n", requestedResource, readErr.Error())
					statusLine = getStatusLine(403)
				} else {
					okResp = true
					statusLine = getStatusLine(200)
	
					fmt.Printf("read %v bytes from file %v...\n", numRead, requestedResource)
					// fmt.Println(string(fileBytes))
				}
			}
		}
	
		conn.Write([]byte(statusLine))
		if okResp {
			conn.Write([]byte(fmt.Sprint("Content-Type: ", mimeForFile(requestedResource), "\r\n")))
			conn.Write([]byte(fmt.Sprint("Content-Length: ", fileSize, "\r\n")))
		}
		conn.Write([]byte("\r\n"))
		conn.Write(fileBytes)
	}
}

func checkMethod(method string) (valid bool) {
	switch (strings.ToUpper(method)) {
		case "GET":
			return true
		default:
			return false
	}
}

func getStatusLine(cd int) (statusLine string) {
	prefix := fmt.Sprint("HTTP/1.1 ", cd)
	switch cd {
		case 200:
			return fmt.Sprint(prefix, " OK\r\n")
		case 403:
			return fmt.Sprint(prefix, " Forbidden\r\n")
		case 404:
			return fmt.Sprint(prefix, " Not Found\r\n")
		case 405:
			return fmt.Sprint(prefix, " Method not Allowed\r\n")
		default: return fmt.Sprint(prefix, "\r\n")
	}
}

type configMap struct {
	port int
	root string
	virtualHosts map[string]string
}

const (
	DEFAULT_PORT = 8080
	DEFAULT_ROOT = "."
	DEFAULT_CONFIG = "./config.properties"
)

// command-line > properties > defaults
func getConfig() configMap {
	var port int
	flag.IntVar(&port, "port", 0, "'port' must be an int")

	var root string
	flag.StringVar(&root, "root", "", "'root' must be a valid path")

	var configPath string
	flag.StringVar(&configPath, "config", "", "'config' must be a valid path")

	flag.Parse()

	if configPath == "" {
		configPath = DEFAULT_CONFIG
	}
	configPath, _ = filepath.Abs(configPath)

	configProperties := properties.MustLoadFile(configPath, properties.UTF8)

	if port == 0 {
		port = configProperties.GetInt("port", DEFAULT_PORT)
	}

	if root == "" {
		root = configProperties.GetString("root", DEFAULT_ROOT)
	}
	root, _ = filepath.Abs(root)

	config := configMap{
		port: port,
		root: root,
		virtualHosts: make(map[string]string),
	}

	for _, key := range configProperties.Keys() {
		if string(key[0]) == "/" {
			config.virtualHosts[key] = configProperties.GetString(key, "")
		}
	}

	return config
}

func mimeForFile(fileName string) (mimeType string) {
	return mime.TypeByExtension(filepath.Ext(fileName))
}
