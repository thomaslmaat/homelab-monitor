package gameservers

import (
	"testing"

	"github.com/thomaslmaat/homelab-monitor/internal/kubernetes"
)

func TestParseEmptyConfiguration(t *testing.T) {
	got, err := Parse("")
	if err != nil || len(got) != 0 { t.Fatalf("Parse empty = %#v, %v", got, err) }
}

func TestParseDefaultsDeployment(t *testing.T) {
	got, err := Parse(`[{"id":"minecraft-main","name":"Minecraft","game":"minecraft","namespace":"games","workload":"minecraft"}]`)
	if err != nil { t.Fatal(err) }
	if len(got) != 1 || got[0].WorkloadType != "deployment" { t.Fatalf("unexpected config: %#v", got) }
}

func TestParseRejectsDuplicateIDs(t *testing.T) {
	_, err := Parse(`[{"id":"same","name":"A","game":"minecraft","namespace":"games","workload":"a"},{"id":"same","name":"B","game":"satisfactory","namespace":"games","workload":"b"}]`)
	if err == nil { t.Fatal("expected duplicate ID error") }
}

func TestParseRejectsUnsupportedWorkloadType(t *testing.T) {
	_, err := Parse(`[{"id":"x","name":"X","game":"minecraft","namespace":"games","workload":"x","workload_type":"statefulset"}]`)
	if err == nil { t.Fatal("expected unsupported workload type error") }
}

func TestResolveUsesExplicitNamespaceAndName(t *testing.T) {
	configs, err := Parse(`[{"id":"minecraft","name":"Minecraft","game":"minecraft","namespace":"games","workload":"minecraft"}]`)
	if err != nil { t.Fatal(err) }
	got := Resolve(configs, []kubernetes.Deployment{{Name:"minecraft",Namespace:"games",Ready:1,Desired:1},{Name:"minecraft",Namespace:"other",Ready:0,Desired:1}})
	if len(got)!=1 || got[0].State!="running" || got[0].Ready!=1 { t.Fatalf("unexpected view: %#v", got) }
}

func TestResolveDoesNotInventMissingWorkload(t *testing.T) {
	configs, _ := Parse(`[{"id":"minecraft","name":"Minecraft","game":"minecraft","namespace":"games","workload":"missing"}]`)
	got := Resolve(configs, nil)
	if got[0].State != "not_found" { t.Fatalf("state = %q", got[0].State) }
}
