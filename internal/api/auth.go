package api

import (
	"fmt"
	"strings"
)

// Forbidden renter identity prefixes. These are host, router, gateway,
// and site credentials, never a renter API key.
// hm_dev_ / hm_site_ / hm_gw_: hypermesh-gateway renter_auth.py.
// hm_rtr_ stays refused: it is not a renter identity.
var ForbiddenRenterPrefixes = []string{"hm_dev_", "hm_rtr_", "hm_gw_", "hm_site_"}

func ValidateRenterKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("api key is required")
	}
	for _, p := range ForbiddenRenterPrefixes {
		if strings.HasPrefix(key, p) {
			return fmt.Errorf("%s is not a renter identity; use an org API key (purpose: renter)", p)
		}
	}
	return nil
}

// RedactKey removes a fixture or caller key from text that would otherwise be printed.
func RedactKey(text, key string) string {
	key = strings.TrimSpace(key)
	if key == "" || text == "" {
		return text
	}
	return strings.ReplaceAll(text, key, "***")
}

func MaskKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}
