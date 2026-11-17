package kubernetes

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
)

func TestDiscoveryRBACExamplesAreReadOnlyAndScoped(t *testing.T) {
	for _, file := range []string{"kubernetes-discovery.yaml", "kubernetes-discovery-cluster.yaml"} {
		t.Run(file, func(t *testing.T) {
			f, err := os.Open(filepath.Join("../../../examples/rbac", file))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			decoder := kyaml.NewYAMLOrJSONDecoder(f, 4096)
			roles, bindings := 0, 0
			for {
				var raw json.RawMessage
				err = decoder.Decode(&raw)
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				var obj struct {
					Kind      string `json:"kind"`
					Metadata  struct{ Name, Namespace string }
					Rules     []rbacv1.PolicyRule
					Subjects  []rbacv1.Subject
					RoleRef   rbacv1.RoleRef
					Automount *bool `json:"automountServiceAccountToken"`
				}
				if err = json.Unmarshal(raw, &obj); err != nil {
					t.Fatal(err)
				}
				switch obj.Kind {
				case "Namespace":
					if obj.Metadata.Name != "consize" {
						t.Fatal("unexpected auth namespace")
					}
				case "ServiceAccount":
					if obj.Metadata.Namespace != "consize" || obj.Metadata.Name != "consize-discovery" || obj.Automount == nil || *obj.Automount {
						t.Fatal("unsafe service account defaults")
					}
				case "Role", "ClusterRole":
					roles++
					if len(obj.Rules) != 1 || !reflect.DeepEqual(obj.Rules[0].APIGroups, []string{"apps"}) || !reflect.DeepEqual(obj.Rules[0].Resources, []string{"deployments"}) || !reflect.DeepEqual(obj.Rules[0].Verbs, []string{"list"}) || len(obj.Rules[0].NonResourceURLs) != 0 || len(obj.Rules[0].ResourceNames) != 0 {
						t.Fatal("excess discovery permission", obj.Rules)
					}
					if obj.Kind == "Role" && obj.Metadata.Namespace != "example-workloads" {
						t.Fatal("role lost namespace boundary")
					}
				case "RoleBinding", "ClusterRoleBinding":
					bindings++
					if len(obj.Subjects) != 1 || obj.Subjects[0].Kind != "ServiceAccount" || obj.Subjects[0].Name != "consize-discovery" || obj.Subjects[0].Namespace != "consize" || obj.RoleRef.Name != "consize-discovery" || obj.RoleRef.APIGroup != rbacv1.GroupName {
						t.Fatal("binding points to unexpected principal")
					}
					if file == "kubernetes-discovery.yaml" && (obj.RoleRef.Kind != "Role" || obj.Metadata.Namespace != "example-workloads") {
						t.Fatal("namespaced binding widened")
					}
					if file == "kubernetes-discovery-cluster.yaml" && obj.RoleRef.Kind != "ClusterRole" {
						t.Fatal("invalid cluster binding")
					}
				default:
					t.Fatal("unexpected RBAC object", obj.Kind)
				}
			}
			if roles != 1 || bindings != 1 {
				t.Fatal("incomplete RBAC example")
			}
		})
	}
}
