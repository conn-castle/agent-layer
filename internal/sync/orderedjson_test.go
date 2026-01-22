package sync

import "testing"

func TestOrderedMapMarshal(t *testing.T) {
	value := OrderedMap[string]{
		"b": "2",
		"a": "1",
	}
	data, err := value.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `{"a":"1","b":"2"}` {
		t.Fatalf("unexpected json: %s", string(data))
	}
}

func TestOrderedMapMarshalNil(t *testing.T) {
	var value OrderedMap[string]
	data, err := value.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "null" {
		t.Fatalf("unexpected json: %s", string(data))
	}
}

func TestOrderedMapMarshalValueError(t *testing.T) {
	// func values cannot be marshaled to JSON
	value := OrderedMap[func()]{"key": func() {}}
	_, err := value.MarshalJSON()
	if err == nil {
		t.Fatalf("expected error for unmarshalable value")
	}
}
