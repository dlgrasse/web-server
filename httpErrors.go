package main

type httpResponse interface {
	Code() int
	Message() string
}
type httpError interface {
	httpResponse
	Error() string
}

func (r *_httpResp) Code() int {
	return r._code
}
func (r *_httpResp) Message() string {
	return r._message
}
func (e *_httpErr) Error() string {
	return e._cause
}

func new200Response() httpResponse {
	return &_httpResp{
		_code:    200,
		_message: "OK",
	}
}
func newForbiddenError(cause string) httpError {
	return createHTTPError(403, cause)
}
func newNotFoundError(cause string) httpError {
	return createHTTPError(404, cause)
}
func newMethodNotAllowedError(cause string) httpError {
	return createHTTPError(405, cause)
}
func newLengthRequiredError(cause string) httpError {
	return createHTTPError(411, cause)
}
func newInternalServerErrorError(cause string) httpError {
	return createHTTPError(500, cause)
}

func createHTTPError(code int, cause string) httpError {
	httpError := _httpErr{_httpResp: _httpResp{_code: code}, _cause: cause}

	switch code {
	case 403:
		httpError._message = "Forbidden"
	case 404:
		httpError._message = "Not Found"
	case 405:
		httpError._message = "Method Not Allowed"
	case 411:
		httpError._message = "Length Required"
	case 500:
		httpError._message = "Internal Server Error"
	default:
		httpError._message = "Unknown"
	}
	return &httpError
}

type _httpResp struct {
	_code    int
	_message string
}
type _httpErr struct {
	_httpResp
	_cause string
}
