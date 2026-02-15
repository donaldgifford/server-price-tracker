package handlers

// ErrorResponse is the standard error response body.
type ErrorResponse struct {
	Error string `json:"error" example:"something went wrong"`
}

// StatusResponse is a generic status response body.
type StatusResponse struct {
	Status string `json:"status" example:"ok"`
}
