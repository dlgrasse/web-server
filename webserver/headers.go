package webserver

import (
	"bytes"
	"github.com/rs/zerolog/log"
	"net"
	"strings"
)

const (
	COOKIE_HEADER     = "Cookie"
	SET_COOKIE_HEADER = "Set-Cookie"
)

type headers interface {
	String() string
	Value(key string) (value string, exists bool)
	Values(key string, delim string) (values []string, exists bool)
	Cookie(name string) (cookie string, exists bool)
	SetCookie(name string, value string)
	ExpireCookie(name string)
	DeleteCookie(name string)
}

func (m _headerMap) String() string {
	hString := ""
	for hKey, hValue := range m {
		hString = hString + hKey + ": " + hValue + "\r\n"
	}
	return hString
}
func (m _headerMap) Value(key string) (value string, exists bool) {
	value, exists = m[key]
	return
}
func (m _headerMap) Values(key string, delim string) (values []string, exists bool) {
	value, exists := m.Value(key)
	if exists {
		values = strings.Split(value, delim)
		return values, true
	} else {
		return nil, false
	}
}
func (m _headerMap) Cookie(name string) (value string, exists bool) {
	cookieValues, exists := m.Values(COOKIE_HEADER, ";")
	if exists {
		for _, cookieValue := range cookieValues {
			cookieParts := strings.Split(cookieValue, "=")
			if strings.TrimSpace(cookieParts[0]) == name {
				return cookieParts[1], true
			}
		}
		return "", false
	} else {
		return "", false
	}
}
func (m _headerMap) SetCookie(name string, value string) {
	m[SET_COOKIE_HEADER] = name + "=" + value + "; Max-Age=100"
}
func (m _headerMap) ExpireCookie(name string) {
	m[SET_COOKIE_HEADER] = name + "=; Max-Age=0"
}
func (m _headerMap) DeleteCookie(name string) {
	cookieValues, exists := m.Values(COOKIE_HEADER, ";")
	if exists {
		for cIdx, cookieValue := range cookieValues {
			cookieParts := strings.Split(cookieValue, "=")
			if cookieParts[0] == name {
				cookieValues = append(cookieValues[:cIdx], cookieValues[cIdx+1:]...)
				break
			}
		}
		m[COOKIE_HEADER] = strings.Join(cookieValues, ";")
	}
}

func parseHeaders(conn net.Conn) (headers, httpError) {
	log.Trace().Msg("#parseHeaders")
	headerMap := make(_headerMap, 10)

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
					headerName := headerRow[0:headerSeparatorIdx]
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

type _headerMap map[string]string
