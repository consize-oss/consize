package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/consize-oss/consize/pkg/plugin"
)

type testPlugin struct{}

func (testPlugin) ID() string { return "test" }
func (testPlugin) Manifest() plugin.Manifest {
	return plugin.Manifest{ID: "test", Category: plugin.CategoryMetrics}
}
func (testPlugin) Health(context.Context) plugin.Health { return plugin.Health{Status: "healthy"} }
func TestProtocolDispatch(t *testing.T) {
	for _, tc := range []struct {
		method   string
		protocol int
		fails    bool
	}{{"handshake", Protocol, false}, {"health", Protocol, false}, {"execute", Protocol, true}, {"unknown", Protocol, true}, {"health", 999, true}} {
		var out bytes.Buffer
		b, _ := json.Marshal(Request{Protocol: tc.protocol, Method: tc.method})
		err := ServeOnce(context.Background(), bytes.NewReader(b), &out, func(json.RawMessage) (plugin.Plugin, error) { return testPlugin{}, nil })
		if err != nil {
			t.Fatal(err)
		}
		var response Response
		if err = json.Unmarshal(out.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if (response.Error != "") != tc.fails {
			t.Fatalf("%s: %+v", tc.method, response)
		}
	}
}
func TestProtocolRejectsMultipleRequests(t *testing.T) {
	var out bytes.Buffer
	if err := ServeOnce(context.Background(), bytes.NewBufferString(`{"protocol":1,"method":"health"} {}`), &out, func(json.RawMessage) (plugin.Plugin, error) { return testPlugin{}, nil }); err == nil {
		t.Fatal("multiple requests accepted")
	}
}
func TestBoundedOutput(t *testing.T) {
	b := &limitedBuffer{limit: 3}
	if _, err := b.Write([]byte("oversize")); err == nil {
		t.Fatal("unbounded output")
	}
}

func TestExecutionEnvelopeNegotiationPreservesLegacyStrictDecoder(t *testing.T) {
	plan := plugin.ActionPlan{PluginID: "action", Execution: &plugin.ExecutionEnvelope{DispatchID: "action-1", PlanDigest: "digest", LeaseEpoch: 7}}
	for _, supported := range []bool{false, true} {
		p := remote{executionEnvelope: supported}
		req := p.prepareRequest(Request{Method: "inspect", Plan: plan})
		wire, err := json.Marshal(req.Plan)
		if err != nil {
			t.Fatal(err)
		}
		// The original v1 plan had only these required fields for this fixture.
		var legacy struct {
			PluginID          string         `json:"plugin_id"`
			ResourceID        string         `json:"resource_id"`
			ActionType        string         `json:"action_type"`
			Summary           string         `json:"summary"`
			Diff              map[string]any `json:"diff"`
			RollbackAvailable bool           `json:"rollback_available"`
			RequiresApproval  bool           `json:"requires_approval"`
		}
		decoder := json.NewDecoder(bytes.NewReader(wire))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&legacy)
		if !supported && err != nil {
			t.Fatalf("legacy executable rejected request: %v", err)
		}
		var modern plugin.ActionPlan
		if err := json.Unmarshal(wire, &modern); err != nil {
			t.Fatal(err)
		}
		if supported && (modern.Execution == nil || modern.Execution.DispatchID != "action-1") {
			t.Fatal("negotiated dispatch identity lost")
		}
		if !supported && modern.Execution != nil {
			t.Fatal("unnegotiated request field sent")
		}
		if plan.Execution == nil {
			t.Fatal("adapter changed the worker's captured plan")
		}
	}
}
