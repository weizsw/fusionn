package handler

import "github.com/gin-gonic/gin"

// errorReportedKey marks a request whose failure a handler or the recovery
// middleware already logged as ERROR.
const errorReportedKey = "fusionn_error_reported"

// MarkErrorReported records that the request's failure is already logged as
// ERROR, so the request summary adds only its status and duration.
func MarkErrorReported(c *gin.Context) { c.Set(errorReportedKey, true) }

// ErrorReported reports whether MarkErrorReported was called for the request.
func ErrorReported(c *gin.Context) bool { return c.GetBool(errorReportedKey) }
