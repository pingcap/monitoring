package bizlogic

import (
	"crypto/sha256"
	"fmt"

	"github.com/gin-gonic/gin"
)

// Record the published content fingerprint without storing rule text in the audit.
func auditSavedConfig(c *gin.Context, data []byte) {
	sum := sha256.Sum256(data)
	c.Set("config_sha256", fmt.Sprintf("%x", sum))
	c.Set("config_bytes", len(data))
	c.Set("config_saved", true)
}
