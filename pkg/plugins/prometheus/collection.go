package prometheus

import (
	"context"
	"strings"
	"time"

	"github.com/consize-oss/consize/pkg/resource"
)

type mappedCollection struct {
	owners    OwnershipSeries
	inventory MappingResult
}

func (p *Plugin) mappedInventory(ctx context.Context, res resource.Resource, scope string, from, to time.Time) (mappedCollection, error) {
	result := mappedCollection{}
	var err error
	if p.mapping.Mode == "owner_chain" {
		result.owners.Pods, err = p.client.QueryRange(ctx, mappingSelector("kube_pod_owner", scope, mappingMatcher(p.mapping.Controller, "=", "true")), from, to, p.step)
		if err != nil {
			return result, safeError(err)
		}
		result.owners.ReplicaSets, err = p.client.QueryRange(ctx, mappingSelector("kube_replicaset_owner", scope, mappingMatcher(p.mapping.Controller, "=", "true")), from, to, p.step)
		if err != nil {
			return result, safeError(err)
		}
	}
	inventory, err := p.client.QueryRange(ctx, mappingInventoryQuery(scope), from, to, p.step)
	if err != nil {
		return result, safeError(err)
	}
	result.inventory = MapSeries(res, "inventory", inventory, result.owners, p.mapping)
	if target, e := targetForMapping(res); e == nil {
		result.inventory = validateInventory(result.inventory, target, p.mapping)
	}
	return result, nil
}
func (p *Plugin) definitions(scope string) map[string]QueryDefinition {
	out := map[string]QueryDefinition{}
	for _, def := range queryCatalog(p.mapping, scope) {
		out[def.Signal] = def
	}
	for signal, query := range p.customQueries {
		def, known := out[signal]
		def.Signal = signal
		def.Query = strings.ReplaceAll(query, "{scope}", scope)
		def.Version = safeCustomVersion(def.Query)
		if !known {
			def.Unit = p.units[signal]
			def.Aggregation = "single-series-custom"
		}
		out[signal] = def
	}
	return out
}
