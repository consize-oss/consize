package api

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/consize-oss/consize/pkg/resource"
)

func parseResourceQuery(values url.Values) (resource.Query, error) {
	q := resource.Query{}
	for key, items := range values {
		if len(items) != 1 || items[0] == "" {
			return q, fmt.Errorf("%w: each filter requires one value", resource.ErrInvalidQuery)
		}
		value := items[0]
		switch key {
		case "provider":
			q.Provider = value
		case "account":
			q.Account = value
		case "region":
			q.Region = value
		case "type":
			q.Type = value
		case "source_plugin_id":
			q.SourcePluginID = value
		case "environment":
			q.Environment = value
		case "lifecycle_state":
			q.LifecycleState = value
		case "support_status":
			q.SupportStatus = value
		case "owner":
			q.Owner = value
		case "cursor":
			q.Cursor = value
		case "limit":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 200 {
				return q, fmt.Errorf("%w: limit must be 1..200", resource.ErrInvalidQuery)
			}
			q.Limit = n
		default:
			return q, fmt.Errorf("%w: unsupported filter", resource.ErrInvalidQuery)
		}
	}
	return q, nil
}
