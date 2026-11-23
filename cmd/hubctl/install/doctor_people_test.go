package install

import "testing"

func TestComparePrincipalSpacePairs(t *testing.T) {
	t.Parallel()
	if err := comparePairs(map[string]string{"alice": "s1"}, map[string]string{"alice": "s1"}); err != nil {
		t.Fatal(err)
	}
	if err := comparePairs(map[string]string{"alice": "s1"}, map[string]string{}); err == nil {
		t.Fatal("orphan principal accepted")
	}
	if err := comparePairs(map[string]string{}, map[string]string{"alice": "s1"}); err == nil {
		t.Fatal("orphan home space accepted")
	}
	if err := comparePairs(map[string]string{"alice": "s2"}, map[string]string{"alice": "s1"}); err == nil {
		t.Fatal("mismatched pair accepted")
	}
}
