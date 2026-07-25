package redis

import "testing"

func TestContractMatrixCoversEveryDeclaredOperation(t *testing.T) {
	for _, spec := range append(append([]toolSpec(nil), readSpecs...), writeSpecs...) {
		for _, operation := range spec.operations {
			if _, ok := contracts[spec.name+"/"+operation]; !ok {
				t.Fatalf("missing contract for %s/%s", spec.name, operation)
			}
		}
	}
	if got, want := len(contracts), 103; got != want {
		t.Fatalf("contract count = %d, want %d", got, want)
	}
}

func TestHashSlotUsesRedisHashTags(t *testing.T) {
	if !sameSlot([]string{"first{tenant}", "second{tenant}"}) {
		t.Fatal("same hash tag must select the same slot")
	}
	if sameSlot([]string{"first", "second"}) {
		t.Fatal("different keys unexpectedly share a slot")
	}
}
