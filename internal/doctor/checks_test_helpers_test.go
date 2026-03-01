package doctor

import "testing"

func requireResultByCheckName(t *testing.T, results []Result, checkName string) Result {
	t.Helper()
	var found *Result
	for _, result := range results {
		if result.CheckName == checkName {
			if found != nil {
				t.Fatalf("multiple %s results in %#v", checkName, results)
			}
			copyResult := result
			found = &copyResult
		}
	}
	if found == nil {
		t.Fatalf("missing %s result in %#v", checkName, results)
	}
	return *found
}
