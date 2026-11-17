package kubernetes

import (
	"net/url"
	"strings"

	"k8s.io/client-go/rest"
)

func validateDiscoveryRESTConfig(c *rest.Config) error {
	if c == nil {
		return scanFailure("configuration")
	}
	u, err := url.Parse(c.Host)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(c.Host, "\r\n\x00") || c.Insecure {
		return scanFailure("configuration")
	}
	return nil
}
