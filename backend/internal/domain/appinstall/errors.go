package appinstall

type requestError struct {
	status  int
	message string
}

func (e *requestError) Error() string       { return e.message }
func (e *requestError) HTTPStatus() int     { return e.status }
func invalidRequest(message string) error   { return &requestError{status: 400, message: message} }
func forbiddenRequest(message string) error { return &requestError{status: 403, message: message} }
