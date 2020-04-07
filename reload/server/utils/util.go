package utils

import (
	"net/http"
	"strings"
)

type ErrorResponse struct {
	ErrorCode    int    `json:"errorCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

type GetParameterFunc func(name string) string

func NewErrorResponse(message string) *ErrorResponse {
	return &ErrorResponse{ErrorCode: http.StatusBadRequest, ErrorMessage: message}
}

func niGetHttpParameter(gpf GetParameterFunc, name string) string {
	return strings.TrimSpace(gpf(name))
}
