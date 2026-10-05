package v2ray

import "errors"

func RandUserAgent() string { return "Nagi/mihomo-subscription" }

// VerifyMethod is intentionally deferred to mihomo validation. The upstream
// converter uses mihomo's cipher registry; Nagi only needs to translate the
// link into a profile mapping and must not reject newer mihomo methods here.
func VerifyMethod(cipher, password string) error {
	if cipher == "" || password == "" {
		return errors.New("missing shadowsocks credentials")
	}
	return nil
}
